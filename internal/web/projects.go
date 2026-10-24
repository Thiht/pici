package web

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/github"
	"github.com/Thiht/pici/internal/stores"
)

type projectsPage struct {
	base
	Projects []stores.Project
}

type projectFields struct {
	RepoURL           string
	Provider          stores.Provider
	DefaultBranch     string
	AuthType          stores.AuthType
	AuthUser          string
	Secret            string
	WebhookPayloadURL string
	WebhookSetupURL   string
}

func (h *Handler) fieldsFromProject(p stores.Project) projectFields {
	fields := projectFields{
		RepoURL:       p.RepoURL,
		Provider:      p.Provider,
		DefaultBranch: p.DefaultBranch,
		AuthType:      p.AuthType,
		AuthUser:      p.AuthUser,
	}
	fields.WebhookPayloadURL = h.webhookPayloadURL(p.Provider, p.Name)
	fields.WebhookSetupURL = webhookSetupURL(p.Provider, p.RepoURL)
	return fields
}

// webhookPayloadURL is the endpoint to paste in the provider's webhook form.
// The project does not have a name yet on the creation form, hence the
// placeholder.
func (h *Handler) webhookPayloadURL(provider stores.Provider, name string) string {
	if provider != stores.ProviderGithub && provider != stores.ProviderGitlab {
		return ""
	}
	project := "<project-name>"
	if name != "" {
		project = url.PathEscape(name)
	}
	return h.publicBaseURL + "/api/webhooks/" + provider.String() + "/" + project
}

// webhookSetupURL is the provider page where the webhook is created. Web
// providers offer no query parameters to prefill that form, so users still
// copy the payload URL and secret from the project form.
func webhookSetupURL(provider stores.Provider, repoURL string) string {
	host, path, ok := github.ParseRemote(repoURL)
	if !ok || host == "" || path == "" {
		return ""
	}
	scheme := "https"
	if strings.HasPrefix(strings.ToLower(repoURL), "http://") {
		scheme = "http"
	}
	switch provider {
	case stores.ProviderGithub:
		return scheme + "://" + host + "/" + path + "/settings/hooks/new"
	case stores.ProviderGitlab:
		return scheme + "://" + host + "/" + path + "/-/hooks"
	default:
		return ""
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
	projects, err := h.projects.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "projects_list", projectsPage{base: h.base(w, r, "Projects", "projects"), Projects: projects})
}

func (h *Handler) ProjectNew(w http.ResponseWriter, r *http.Request) {
	project := stores.Project{Provider: stores.ProviderGeneric, AuthType: stores.AuthTypeNone}
	data := projectFormPage{base: h.base(w, r, "New project", "projects"), Project: project, Fields: h.fieldsFromProject(project)}
	h.render(w, r, http.StatusOK, "project_form", data)
}

func (h *Handler) ProjectCreate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	draft := projectFromForm(r, stores.Project{})
	created, err := h.projects.Create(r.Context(), draft)
	if errors.Is(err, app.ErrInvalid) {
		fields := h.fieldsFromProject(draft)
		fields.Secret = draft.AuthSecret
		data := projectFormPage{base: h.base(w, r, "New project", "projects"), Project: draft, Fields: fields, Error: err.Error()}
		h.render(w, r, http.StatusBadRequest, "project_form", data)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Project created.")
	redirect(w, r, "/projects/"+created.ID.String())
}

func (h *Handler) ProjectShow(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	data := projectFormPage{base: h.base(w, r, "Edit "+project.Name, "projects"), Project: project, Fields: h.fieldsFromProject(project), Editing: true}
	h.render(w, r, http.StatusOK, "project_form", data)
}

func (h *Handler) ProjectUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	draft := projectFromForm(r, project)
	if _, err := h.projects.Update(r.Context(), draft); err != nil {
		if errors.Is(err, app.ErrInvalid) {
			fields := h.fieldsFromProject(draft)
			fields.Secret = draft.AuthSecret
			data := projectFormPage{base: h.base(w, r, "Edit "+project.Name, "projects"), Project: draft, Fields: fields, Editing: true, Error: err.Error()}
			h.render(w, r, http.StatusBadRequest, "project_form", data)
			return
		}
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	if err := h.projects.Delete(r.Context(), project.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Project deleted.")
	redirect(w, r, "/")
}

func (h *Handler) ProjectConfigs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	dir := h.workspace.DiscoveryDir(project.ID)
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		ref = project.DefaultBranch
	}
	workflows, err := ci.DiscoverProjectWorkflows(r.Context(), project, dir, ref)
	if err == nil && ref == project.DefaultBranch {
		err = ci.SyncProjectSchedules(r.Context(), h.store, project.ID, dir)
	}
	h.render(w, r, http.StatusOK, "workflows", struct {
		Workflows []string
		Error     string
	}{Workflows: workflows, Error: errorText(err)})
}

func (h *Handler) ProjectRefs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	refs, err := git.ListRefs(r.Context(), project.RepoURL, projects.Auth(project))
	h.render(w, r, http.StatusOK, "refs", struct {
		ProjectID string
		Branches  []string
		Tags      []string
		Default   string
		Error     string
	}{ProjectID: project.ID.String(), Branches: refs.Branches, Tags: refs.Tags, Default: project.DefaultBranch, Error: errorText(err)})
}

func (h *Handler) ProjectDetect(w http.ResponseWriter, r *http.Request) {
	fields := projectFields{
		RepoURL:       strings.TrimSpace(r.FormValue("repo_url")),
		Provider:      stores.Provider(r.FormValue("provider")),
		DefaultBranch: strings.TrimSpace(r.FormValue("default_branch")),
		AuthType:      stores.AuthType(r.FormValue("auth_type")),
		AuthUser:      strings.TrimSpace(r.FormValue("auth_user")),
		Secret:        r.FormValue("auth_secret"),
	}
	detected := h.projects.Detect(r.Context(), stores.Project{
		Name:          strings.TrimSpace(r.FormValue("name")),
		RepoURL:       fields.RepoURL,
		Provider:      fields.Provider,
		AuthType:      fields.AuthType,
		AuthUser:      fields.AuthUser,
		AuthSecret:    fields.Secret,
		DefaultBranch: fields.DefaultBranch,
	})
	fields.Provider = detected.Provider
	fields.AuthUser = detected.AuthUser
	fields.DefaultBranch = detected.DefaultBranch
	fields.WebhookPayloadURL = h.webhookPayloadURL(detected.Provider, detected.Name)
	fields.WebhookSetupURL = webhookSetupURL(detected.Provider, detected.RepoURL)
	h.render(w, r, http.StatusOK, "project_detect", fields)
}

func projectFromForm(r *http.Request, p stores.Project) stores.Project {
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

	return p
}
