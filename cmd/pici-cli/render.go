package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Thiht/pici/client"
)

type format int

const (
	formatAuto format = iota
	formatJSON
	formatPretty
)

var formatFlag = formatAuto

func parseGlobalFlags(args []string) ([]string, error) {
	var out []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			formatFlag = formatJSON
		case args[i] == "--format":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--format requires a value (json|pretty)")
			}
			if err := setFormat(args[i]); err != nil {
				return nil, err
			}
		case strings.HasPrefix(args[i], "--format="):
			if err := setFormat(strings.TrimPrefix(args[i], "--format=")); err != nil {
				return nil, err
			}
		default:
			out = append(out, args[i])
		}
	}
	return out, nil
}

func setFormat(s string) error {
	switch s {
	case "json":
		formatFlag = formatJSON
	case "pretty":
		formatFlag = formatPretty
	default:
		return fmt.Errorf("invalid --format %q (json|pretty)", s)
	}
	return nil
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func jsonOutput() bool {
	switch formatFlag {
	case formatJSON:
		return true
	case formatPretty:
		return false
	default:
		return !isTTY()
	}
}

func output(v any) error {
	if jsonOutput() {
		return printJSON(v)
	}
	switch x := v.(type) {
	case client.Execution:
		printExecution(x)
	case []client.Execution:
		printExecutions(x)
	case client.Project:
		printProject(x)
	case []client.Project:
		printProjects(x)
	case []client.Variable:
		printVars(x)
	case []client.Artifact:
		printArtifacts(x)
	default:
		return printJSON(v)
	}
	return nil
}

const (
	reset = "\x1b[0m"
	dim   = "\x1b[2m"
	red   = "\x1b[31m"
	green = "\x1b[32m"
	cyan  = "\x1b[36m"
)

func paint(s, color string) string {
	if !isTTY() || color == "" {
		return s
	}
	return color + s + reset
}

func statusColor(s string) string {
	switch s {
	case "success":
		return green
	case "failed":
		return red
	case "running":
		return cyan
	default:
		return dim
	}
}

func statusSymbol(s string) string {
	switch s {
	case "success":
		return "✓"
	case "failed":
		return "✗"
	case "running":
		return "▶"
	case "pending":
		return "○"
	case "canceled":
		return "⊘"
	case "skipped":
		return "-"
	default:
		return "?"
	}
}

func visibleWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			i++
			continue
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
		w++
	}
	return w
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-visibleWidth(s)))
}

func duration(start, end *time.Time) string {
	if start == nil {
		return "-"
	}
	if end == nil {
		return time.Since(*start).Round(time.Second).String()
	}
	return end.Sub(*start).Round(time.Second).String()
}

func printExecution(e client.Execution) {
	status := string(e.Status)
	title := e.Workflow + "@" + e.Ref
	if e.Project != "" {
		title = e.Project + "  " + title
	}
	fmt.Printf("%s  %s  %s\n",
		paint(statusSymbol(status)+" "+status, statusColor(status)),
		title,
		duration(e.StartedAt, e.FinishedAt),
	)

	w := 0
	for _, s := range e.Steps {
		w = max(w, visibleWidth(s.Name))
	}
	for _, s := range e.Steps {
		st := string(s.Status)
		line := fmt.Sprintf("  %s %s  %s", paint(statusSymbol(st), statusColor(st)), pad(s.Name, w), duration(s.StartedAt, s.FinishedAt))
		if s.Error != "" {
			line += "  " + paint(s.Error, red)
		}
		fmt.Println(line)
	}
}

func printExecutions(list []client.Execution) {
	for _, e := range list {
		status := string(e.Status)
		fmt.Printf("%s  %-28s  %s  %s\n",
			paint(statusSymbol(status)+" "+status, statusColor(status)),
			e.Workflow+"@"+e.Ref,
			e.ID.String()[:8],
			e.CreatedAt.Local().Format("2006-01-02 15:04"),
		)
	}
}

func printProjects(projects []client.Project) {
	for _, p := range projects {
		fmt.Printf("%-24s  %-8s  %s\n", p.Name, p.Provider, p.RepoURL)
	}
}

func printProject(p client.Project) {
	fmt.Printf("id:             %s\n", p.ID)
	fmt.Printf("name:           %s\n", p.Name)
	fmt.Printf("repo_url:       %s\n", p.RepoURL)
	fmt.Printf("provider:       %s\n", p.Provider)
	fmt.Printf("auth_type:      %s\n", p.AuthType)
	if p.AuthUser != "" {
		fmt.Printf("auth_user:      %s\n", p.AuthUser)
	}
	fmt.Printf("default_branch: %s\n", p.DefaultBranch)
}

func printVars(vars []client.Variable) {
	for _, v := range vars {
		value := v.Value
		if v.Secret {
			value = "••••••"
		}
		fmt.Printf("%s=%s", v.Key, value)
		if v.Secret {
			fmt.Print("  [secret]")
		}
		fmt.Println()
	}
}

func printArtifacts(list []client.Artifact) {
	for _, a := range list {
		fmt.Printf("%s/%s  %s\n", a.Step, a.Path, humanBytes(a.Size))
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
