package web

import (
	"html/template"

	terminal "github.com/buildkite/terminal-to-html/v3"
)

func ansiToHTML(s string) template.HTML {
	return template.HTML(terminal.Render([]byte(s)))
}
