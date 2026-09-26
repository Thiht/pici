package web

import (
	"html/template"

	terminal "github.com/buildkite/terminal-to-html/v3"
)

// ansiToHTML renders terminal output containing ANSI escape sequences as safe
// HTML. It is a thin wrapper around terminal-to-html, which emulates a terminal
// (SGR colours, cursor movement, OSC 8 links) and escapes the text it emits.
// The styling classes it produces (term-*) are defined in terminal.css.
func ansiToHTML(s string) template.HTML {
	return template.HTML(terminal.Render([]byte(s)))
}
