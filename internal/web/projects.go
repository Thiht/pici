package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/stores"
)

type projectsPage struct {
	base
	Projects []stores.Project
}

type projectFields struct {
	RepoURL       string
	Provider      stores.Provider
	DefaultBranch string
	AuthType      stores.AuthType
	AuthUser      string
	Secret        string
}

func fieldsFromProject(p stores.Project) projectFields {
	return projectFields{
		RepoURL:       p.RepoURL,
		Provider:      p.Provider,
		DefaultBranch: p.DefaultBranch,
		AuthType:      p.AuthType,
		AuthUser:      p.AuthUser,
	}
}

type projectFormPage struct {
	base
	Project stores.Project
	Fields  projectFields
	Editing bool
	Error   string
}

type projectPage struct {
	base
	Project        stores.Project
	Executions     []stores.Execution
	Statuses       []string
	WorkflowNames  []string
	StatusFilter   string
	WorkflowFilter string
}

type projectVariablesPage struct {
	base
	Project         stores.Project
	Variables       []stores.Variable
	GlobalVariables []stores.Variable
	Overridden      map[string]bool
}

func (h *Handler) ProjectsList(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.ListProjects(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "projects_list", projectsPage{base: h.base(w, r, "Projects", "projects"), Projects: projects})
}

func (h *Handler) ProjectNew(w http.ResponseWriter, r *http.Request) {
	project := stores.Project{Provider: stores.ProviderGeneric, AuthType: stores.AuthTypeNone}
	data := projectFormPage{base: h.base(w, r, "New project", "projects"), Project: project, Fields: fieldsFromProject(project)}
	h.render(w, r, http.StatusOK, "project_form", data)
}

func (h *Handler) ProjectCreate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, errMsg := projectFromForm(r, stores.Project{})
	if errMsg != "" {
		fields := fieldsFromProject(project)
		fields.Secret = project.AuthSecret
		data := projectFormPage{base: h.base(w, r, "New project", "projects"), Project: project, Fields: fields, Error: errMsg}
		h.render(w, r, http.StatusBadRequest, "project_form", data)
		return
	}
	project.ID = uuid.New()
	project.CreatedAt = time.Now()
	created, err := h.store.CreateProject(r.Context(), project)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Project created.")
	redirect(w, r, "/projects/"+created.ID.String())
}

func (h *Handler) ProjectShow(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	executions, err := h.store.ListExecutions(r.Context(), project.ID, 100)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	statusFilter := r.URL.Query().Get("status")
	workflowFilter := r.URL.Query().Get("workflow")
	data := projectPage{
		base:           h.base(w, r, project.Name, "projects"),
		Project:        project,
		Executions:     filterExecutions(executions, statusFilter, workflowFilter),
		Statuses:       stores.StatusNames(),
		WorkflowNames:  distinctWorkflows(executions),
		StatusFilter:   statusFilter,
		WorkflowFilter: workflowFilter,
	}
	h.render(w, r, http.StatusOK, "project_show", data)
}

func filterExecutions(executions []stores.Execution, status, workflow string) []stores.Execution {
	if status == "" && workflow == "" {
		return executions
	}
	out := make([]stores.Execution, 0, len(executions))
	for _, e := range executions {
		if status != "" && e.Status.String() != status {
			continue
		}
		if workflow != "" && e.Workflow != workflow {
			continue
		}
		out = append(out, e)
	}
	return out
}

func distinctWorkflows(executions []stores.Execution) []string {
	seen := make(map[string]bool, len(executions))
	out := make([]string, 0, len(executions))
	for _, e := range executions {
		if !seen[e.Workflow] {
			seen[e.Workflow] = true
			out = append(out, e.Workflow)
		}
	}
	sort.Strings(out)
	return out
}

func (h *Handler) ProjectEdit(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	data := projectFormPage{base: h.base(w, r, "Edit "+project.Name, "projects"), Project: project, Fields: fieldsFromProject(project), Editing: true}
	h.render(w, r, http.StatusOK, "project_form", data)
}

func (h *Handler) ProjectUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	updated, errMsg := projectFromForm(r, project)
	if errMsg != "" {
		fields := fieldsFromProject(updated)
		fields.Secret = updated.AuthSecret
		data := projectFormPage{base: h.base(w, r, "Edit "+project.Name, "projects"), Project: updated, Fields: fields, Editing: true, Error: errMsg}
		h.render(w, r, http.StatusBadRequest, "project_form", data)
		return
	}
	updated.UpdatedAt = time.Now()
	if _, err := h.store.UpdateProject(r.Context(), updated); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Project updated.")
	redirect(w, r, "/projects/"+project.ID.String())
}

func (h *Handler) ProjectDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	if err := h.store.DeleteProject(r.Context(), project.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Project deleted.")
	redirect(w, r, "/")
}

func (h *Handler) ProjectConfigs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	dir := h.workspaceDir + "/" + project.ID.String() + "/_discovery"
	workflows, err := ci.DiscoverProjectWorkflows(r.Context(), project, dir, project.DefaultBranch)
	if err == nil {
		err = ci.SyncProjectSchedules(r.Context(), h.store, project.ID, dir)
	}
	h.render(w, r, http.StatusOK, "workflows", struct {
		Workflows []string
		Error     string
	}{Workflows: workflows, Error: errorText(err)})
}

func (h *Handler) ProjectRefs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	refs, err := git.ListRefs(r.Context(), project.RepoURL, git.Auth{
		Type:   project.AuthType.String(),
		User:   project.AuthUser,
		Secret: project.AuthSecret,
	})
	h.render(w, r, http.StatusOK, "refs", struct {
		Branches []string
		Tags     []string
		Default  string
		Error    string
	}{Branches: refs.Branches, Tags: refs.Tags, Default: project.DefaultBranch, Error: errorText(err)})
}

// ProjectDetect fills in the fields that can be guessed from the repository
// URL (provider, default branch) and the auth type (auth user). It renders the
// project_fields fragment, swapped in place by htmx as the user types.
func (h *Handler) ProjectDetect(w http.ResponseWriter, r *http.Request) {
	fields := projectFields{
		RepoURL:       strings.TrimSpace(r.FormValue("repo_url")),
		Provider:      stores.Provider(r.FormValue("provider")),
		DefaultBranch: strings.TrimSpace(r.FormValue("default_branch")),
		AuthType:      stores.AuthType(r.FormValue("auth_type")),
		AuthUser:      strings.TrimSpace(r.FormValue("auth_user")),
		Secret:        r.FormValue("auth_secret"),
	}
	if fields.RepoURL != "" {
		fields.Provider = inferProvider(fields.RepoURL)
	}
	if fields.AuthUser == "" || fields.AuthUser == "oauth2" || fields.AuthUser == "git" {
		fields.AuthUser = defaultAuthUser(fields.AuthType)
	}
	if fields.DefaultBranch == "" && fields.RepoURL != "" {
		refs, err := git.ListRefs(r.Context(), fields.RepoURL, git.Auth{
			Type:   fields.AuthType.String(),
			User:   fields.AuthUser,
			Secret: fields.Secret,
		})
		if err == nil {
			fields.DefaultBranch = refs.Head
		}
	}
	h.render(w, r, http.StatusOK, "project_detect", fields)
}

// projectFromForm applies the submitted fields on top of a base project and
// validates them, returning a human-readable message on failure.
func projectFromForm(r *http.Request, p stores.Project) (stores.Project, string) {
	p.Name = strings.TrimSpace(r.FormValue("name"))
	p.RepoURL = strings.TrimSpace(r.FormValue("repo_url"))
	p.Provider = stores.Provider(r.FormValue("provider"))
	p.AuthType = stores.AuthType(r.FormValue("auth_type"))
	p.AuthUser = strings.TrimSpace(r.FormValue("auth_user"))
	p.DefaultBranch = strings.TrimSpace(r.FormValue("default_branch"))

	if secret := r.FormValue("auth_secret"); secret != "" {
		p.AuthSecret = secret
	}
	if f, _, err := r.FormFile("auth_secret_file"); err == nil {
		defer f.Close()
		if b, err := io.ReadAll(f); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			p.AuthSecret = string(b)
		}
	}
	if secret := r.FormValue("webhook_secret"); secret != "" {
		p.WebhookSecret = secret
	}

	if p.Name == "" {
		return p, "Name is required."
	}
	if p.RepoURL == "" {
		return p, "Repository URL is required."
	}
	if p.Provider == "" || !p.Provider.IsValid() {
		p.Provider = inferProvider(p.RepoURL)
	}
	if p.AuthType == "" || !p.AuthType.IsValid() {
		p.AuthType = stores.AuthTypeNone
	}
	return p, ""
}

func inferProvider(repoURL string) stores.Provider {
	switch {
	case strings.Contains(repoURL, "github.com"):
		return stores.ProviderGithub
	case strings.Contains(repoURL, "gitlab"):
		return stores.ProviderGitlab
	default:
		return stores.ProviderGeneric
	}
}

func defaultAuthUser(authType stores.AuthType) string {
	switch authType {
	case stores.AuthTypeToken:
		return "oauth2"
	case stores.AuthTypeSsh:
		return "git"
	default:
		return ""
	}
}

func resolveProject(ctx context.Context, store stores.Store, idOrName string) (stores.Project, error) {
	if id, err := uuid.Parse(idOrName); err == nil {
		if p, err := store.GetProject(ctx, id); err == nil {
			return p, nil
		} else if !errors.Is(err, stores.ErrNotFound) {
			return stores.Project{}, err
		}
	}
	return store.GetProjectByName(ctx, idOrName)
}

func maskVariables(variables []stores.Variable) []stores.Variable {
	out := make([]stores.Variable, len(variables))
	copy(out, variables)
	for i := range out {
		if out[i].Secret {
			out[i].Value = ""
		}
	}
	return out
}
