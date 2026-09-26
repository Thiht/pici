// Package web serves the embedded management UI. It is an independent
// front-end over the same store and runner as the JSON API: it renders Go
// templates, enhanced with htmx, and never round-trips through the API.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/middlewares"
	"github.com/Thiht/pici/internal/stores"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

const (
	csrfCookie  = "pici_csrf"
	flashCookie = "pici_flash"
)

var funcs = template.FuncMap{
	"statusClass":   statusClass,
	"formatTime":    formatTime,
	"formatTimePtr": formatTimePtr,
	"duration":      duration,
	"since":         since,
	"size":          humanSize,
	"joined":        strings.Join,
	"ansi":          ansiToHTML,
	"capitalize":    capitalize,
}

var templates = template.Must(template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html"))

var staticRoot = mustSub(staticFS, "static")

// assetVersion busts browser caches whenever the compiled CSS changes.
var assetVersion = func() string {
	b, err := fs.ReadFile(staticRoot, "app.css")
	if err != nil {
		return "dev"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}()

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

type Handler struct {
	store        stores.Store
	runner       *ci.Runner
	workspaceDir string
	buildVersion string
	apiToken     string
	csrfEnabled  bool
}

func New(store stores.Store, runner *ci.Runner, workspaceDir, apiToken, buildVersion string) *Handler {
	return &Handler{
		store:        store,
		runner:       runner,
		workspaceDir: workspaceDir,
		buildVersion: buildVersion,
		apiToken:     apiToken,
		csrfEnabled:  apiToken != "",
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("POST /logout", h.Logout)

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticRoot)))

	mux.HandleFunc("GET /{$}", h.ProjectsList)
	mux.HandleFunc("GET /projects/new", h.ProjectNew)
	mux.HandleFunc("POST /projects", h.ProjectCreate)
	mux.HandleFunc("POST /projects/detect", h.ProjectDetect)
	mux.HandleFunc("GET /projects/{id}", h.ProjectShow)
	mux.HandleFunc("GET /projects/{id}/edit", h.ProjectEdit)
	mux.HandleFunc("POST /projects/{id}", h.ProjectUpdate)
	mux.HandleFunc("POST /projects/{id}/delete", h.ProjectDelete)
	mux.HandleFunc("GET /projects/{id}/configs", h.ProjectConfigs)
	mux.HandleFunc("GET /projects/{id}/refs", h.ProjectRefs)
	mux.HandleFunc("GET /projects/{id}/cache", h.ProjectCache)
	mux.HandleFunc("POST /projects/{id}/cache/images/delete", h.ProjectCacheImageDelete)
	mux.HandleFunc("POST /projects/{id}/cache/volumes/delete", h.ProjectCacheVolumeDelete)

	mux.HandleFunc("GET /projects/{id}/variables", h.ProjectVariables)
	mux.HandleFunc("POST /projects/{id}/variables", h.ProjectVariableSet)
	mux.HandleFunc("POST /projects/{id}/variables/{key}/delete", h.ProjectVariableDelete)
	mux.HandleFunc("GET /variables", h.GlobalVariables)
	mux.HandleFunc("POST /variables", h.GlobalVariableSet)
	mux.HandleFunc("POST /variables/{key}/delete", h.GlobalVariableDelete)

	mux.HandleFunc("POST /projects/{id}/executions", h.ExecutionCreate)
	mux.HandleFunc("GET /executions/{id}", h.ExecutionShow)
	mux.HandleFunc("GET /executions/{id}/logs", h.ExecutionLogs)
	mux.HandleFunc("GET /executions/{id}/steps/{step}/logs", h.StepLogs)
	mux.HandleFunc("POST /executions/{id}/cancel", h.ExecutionCancel)
	mux.HandleFunc("POST /executions/{id}/rebuild", h.ExecutionRebuild)
	mux.HandleFunc("GET /executions/{id}/artifacts/{step}/{path...}", h.ArtifactDownload)

	return middlewares.Log(middlewares.Auth(h.apiToken, mux))
}

// base is embedded in every page model. It carries the shared chrome data.
type base struct {
	Title        string
	Active       string
	CSRFToken    string
	FlashKind    string
	FlashText    string
	Version      string
	AssetVersion string
}

func (h *Handler) base(w http.ResponseWriter, r *http.Request, title, active string) base {
	b := base{Title: title, Active: active, CSRFToken: h.ensureCSRF(w, r), Version: h.buildVersion, AssetVersion: assetVersion}
	if c, err := r.Cookie(flashCookie); err == nil && c.Value != "" {
		kind, text, _ := strings.Cut(c.Value, ":")
		if decoded, err := url.QueryUnescape(text); err == nil {
			text = decoded
		}
		b.FlashKind, b.FlashText = kind, text
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	}
	return b
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render template", "template", name, "error", err)
	}
}

// redirect performs a client-side (htmx) or server-side redirect. Every
// mutation responds with one so that htmx requests and native form posts
// behave the same way.
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func setFlash(w http.ResponseWriter, kind, text string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    kind + ":" + url.QueryEscape(text),
		Path:     "/",
		MaxAge:   60,
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
}

func (h *Handler) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if !h.csrfEnabled {
		return ""
	}
	if c, err := r.Cookie(csrfCookie); err == nil && c.Value != "" {
		return c.Value
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func (h *Handler) validCSRF(r *http.Request) bool {
	if !h.csrfEnabled {
		return true
	}
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("_csrf"))) == 1
}

func statusClass(status any) string {
	switch fmt.Sprint(status) {
	case "success":
		return "badge badge-soft badge-success"
	case "failed":
		return "badge badge-soft badge-error"
	case "running":
		return "badge badge-soft badge-info"
	case "pending":
		return "badge badge-soft badge-warning"
	default:
		return "badge badge-soft badge-neutral"
	}
}

func capitalize(v any) string {
	s := fmt.Sprint(v)
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return formatTime(*t)
}

func duration(start, end any) string {
	s, ok := start.(time.Time)
	if !ok {
		p, ok := start.(*time.Time)
		if !ok || p == nil {
			return "—"
		}
		s = *p
	}
	e, ok := end.(time.Time)
	if !ok {
		p, ok := end.(*time.Time)
		if !ok || p == nil {
			return "—"
		}
		e = *p
	}
	return e.Sub(s).Round(time.Second).String()
}

func since(t *time.Time) string {
	if t == nil {
		return "—"
	}
	d := time.Since(*t).Round(time.Second)
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}
