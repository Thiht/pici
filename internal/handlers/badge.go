package handlers

import (
	"fmt"
	"html"
	"net/http"

	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

// Badge renders a shields.io-style SVG status badge for a project. The
// workflow can be selected with ?workflow=; without it the newest execution of
// any workflow is used. The label defaults to the workflow (or project) name
// and can be overridden with ?label=. The endpoint is public so badges can be
// embedded in READMEs.
func (h *ProjectsHandler) Badge(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	workflow := r.URL.Query().Get("workflow")
	executions, err := h.store.ListExecutions(r.Context(), project.ID, 50)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}

	status := ""
	for _, e := range executions {
		if workflow == "" || e.Workflow == workflow {
			status = string(e.Status)
			break
		}
	}

	label := r.URL.Query().Get("label")
	if label == "" {
		label = workflow
	}
	if label == "" {
		label = project.Name
	}

	value, color := badgeStatus(status)
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, badgeSVG(label, value, color))
}

func badgeStatus(status string) (value, color string) {
	switch stores.Status(status) {
	case stores.StatusSuccess:
		return "passing", "#2ea44f"
	case stores.StatusFailed:
		return "failing", "#d73a49"
	case stores.StatusRunning:
		return "running", "#0969da"
	case stores.StatusPending:
		return "pending", "#dbab09"
	case stores.StatusCanceled:
		return "canceled", "#6e7781"
	default:
		return "unknown", "#9f9f9f"
	}
}

func badgeSVG(label, value, color string) string {
	label, value = html.EscapeString(label), html.EscapeString(value)
	labelW := 7*len(label) + 10
	valueW := 7*len(value) + 10
	if labelW < 20 {
		labelW = 20
	}
	if valueW < 20 {
		valueW = 20
	}
	total := labelW + valueW
	labelCenter := labelW / 2
	valueCenter := labelW + valueW/2
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <title>%s: %s</title>
  <linearGradient id="s" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
    <text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>`,
		total, label, value,
		label, value,
		total,
		labelW,
		labelW, valueW, color,
		total,
		labelCenter, label,
		labelCenter, label,
		valueCenter, value,
		valueCenter, value,
	)
}
