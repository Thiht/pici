package web

import (
	"fmt"
	"html"
	"html/template"
	"strconv"
	"strings"
)

// ansiToHTML renders a log line containing ANSI SGR escape sequences as safe
// HTML. Text is HTML-escaped; only SGR styling is translated, all other escape
// sequences are dropped.
func ansiToHTML(s string) template.HTML {
	var b strings.Builder
	var fg, bg string
	var bold, dim, italic, underline bool

	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j >= len(s) {
				break
			}
			if s[j] == 'm' {
				applySGR(s[i+2:j], &fg, &bg, &bold, &dim, &italic, &underline)
			}
			i = j + 1
			continue
		}
		j := i
		for j < len(s) && s[j] != 0x1b {
			j++
		}
		b.WriteString(styleRun(s[i:j], fg, bg, bold, dim, italic, underline))
		i = j
	}
	return template.HTML(b.String())
}

func applySGR(params string, fg, bg *string, bold, dim, italic, underline *bool) {
	if params == "" {
		params = "0"
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			n = 0
		}
		switch {
		case n == 0:
			*fg, *bg = "", ""
			*bold, *dim, *italic, *underline = false, false, false, false
		case n == 1:
			*bold = true
		case n == 2:
			*dim = true
		case n == 3:
			*italic = true
		case n == 4:
			*underline = true
		case n == 22:
			*bold, *dim = false, false
		case n == 23:
			*italic = false
		case n == 24:
			*underline = false
		case n >= 30 && n <= 37:
			*fg = ansiPalette[n-30]
		case n == 39:
			*fg = ""
		case n >= 40 && n <= 47:
			*bg = ansiPalette[n-40]
		case n == 49:
			*bg = ""
		case n >= 90 && n <= 97:
			*fg = ansiPalette[n-90+8]
		case n >= 100 && n <= 107:
			*bg = ansiPalette[n-100+8]
		case n == 38 || n == 48:
			color, consumed := extendedColor(parts[i+1:])
			if n == 38 {
				*fg = color
			} else {
				*bg = color
			}
			i += consumed
		}
	}
}

func extendedColor(rest []string) (string, int) {
	if len(rest) == 0 {
		return "", 0
	}
	switch rest[0] {
	case "5":
		if len(rest) < 2 {
			return "", 1
		}
		idx, _ := strconv.Atoi(rest[1])
		return ansi256(idx), 2
	case "2":
		if len(rest) < 4 {
			return "", len(rest)
		}
		r, _ := strconv.Atoi(rest[1])
		g, _ := strconv.Atoi(rest[2])
		b, _ := strconv.Atoi(rest[3])
		return fmt.Sprintf("rgb(%d,%d,%d)", r, g, b), 4
	default:
		return "", 1
	}
}

func styleRun(text, fg, bg string, bold, dim, italic, underline bool) string {
	if text == "" {
		return ""
	}
	escaped := html.EscapeString(text)
	var styles []string
	if fg != "" {
		styles = append(styles, "color:"+fg)
	}
	if bg != "" {
		styles = append(styles, "background-color:"+bg)
	}
	if bold {
		styles = append(styles, "font-weight:700")
	}
	if dim {
		styles = append(styles, "opacity:.7")
	}
	if italic {
		styles = append(styles, "font-style:italic")
	}
	if underline {
		styles = append(styles, "text-decoration:underline")
	}
	if len(styles) == 0 {
		return escaped
	}
	return `<span style="` + strings.Join(styles, ";") + `">` + escaped + `</span>`
}

// ansiPalette is the standard 16-colour palette (8 normal, 8 bright).
var ansiPalette = [16]string{
	"#000000", "#cd3131", "#0dbc79", "#e5e510",
	"#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
	"#666666", "#f14c4c", "#23d18b", "#f5f543",
	"#3b8eea", "#d670d6", "#29b8db", "#e5e5e5",
}

func ansi256(idx int) string {
	if idx < 0 {
		idx = 0
	}
	if idx < 16 {
		return ansiPalette[idx]
	}
	if idx < 232 {
		idx -= 16
		steps := [6]int{0, 95, 135, 175, 215, 255}
		r := steps[idx/36]
		g := steps[(idx/6)%6]
		b := steps[idx%6]
		return fmt.Sprintf("rgb(%d,%d,%d)", r, g, b)
	}
	if idx > 255 {
		idx = 255
	}
	v := 8 + (idx-232)*10
	return fmt.Sprintf("rgb(%d,%d,%d)", v, v, v)
}
