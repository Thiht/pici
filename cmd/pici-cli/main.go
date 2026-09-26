package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Thiht/pici/client"
)

func main() {
	args, err := parseGlobalFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	c := client.New(cmp.Or(os.Getenv("PICI_ADDR"), "http://localhost:8080"), os.Getenv("PICI_TOKEN"))

	switch args[0] {
	case "run":
		err = cmdRun(c, args[1:])
	case "logs":
		err = cmdLogs(c, args[1:])
	case "status":
		err = cmdStatus(c, args[1:])
	case "projects":
		err = cmdProjects(c, args[1:])
	case "executions":
		err = cmdExecutions(c, args[1:])
	case "vars":
		err = cmdVars(c, args[1:])
	case "cancel":
		err = cmdCancel(c, args[1:])
	case "rebuild":
		err = cmdRebuild(c, args[1:])
	case "artifacts":
		err = cmdArtifacts(c, args[1:])
	case "validate":
		err = cmdValidate(c, args[1:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`pici — minimal CI client

Usage:
  pici-cli run <project> <workflow> [--ref <ref>]
  pici-cli logs <execution-id> [--follow]
  pici-cli status <execution-id>
  pici-cli projects
  pici-cli projects add <name> <repo_url> [flags]
  pici-cli projects show <name-or-id>
  pici-cli projects update <name-or-id> [flags]
  pici-cli projects rm <name-or-id>
  pici-cli executions <project> [--limit N]
  pici-cli vars [--project <name>]
  pici-cli vars set <key> <value> [--project <name>] [--secret]
  pici-cli vars rm <key> [--project <name>]
  pici-cli cancel <execution-id>
  pici-cli rebuild <execution-id>
  pici-cli artifacts <execution-id>
  pici-cli artifacts get <execution-id> <step>/<path>
  pici-cli validate <ci.yml>

Global flags:
  --json             force JSON output
  --format json|pretty   force output format (default: pretty on a TTY, JSON otherwise)

Project flags (add/update):
  --provider X  --auth-type X  --auth-user X  --auth-secret X
  --webhook-secret X  --default-branch X  (update: also --name, --repo-url)

Env:
  PICI_ADDR   server base URL (default http://localhost:8080)
  PICI_TOKEN  API token (sent as Authorization: Bearer)
`)
}

func cmdRun(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	ref := fs.String("ref", "", "git ref to build")
	if err := fs.Parse(intersperse(args)); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("usage: pici-cli run <project> <workflow> [--ref <ref>]")
	}
	exec, err := c.TriggerExecution(context.Background(), fs.Arg(0), fs.Arg(1), *ref)
	if err != nil {
		return err
	}
	fmt.Println(exec.ID)
	return nil
}

func cmdStatus(c *client.Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pici-cli status <execution-id>")
	}
	exec, err := c.GetExecution(context.Background(), args[0])
	if err != nil {
		return err
	}
	return output(exec)
}

func cmdLogs(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("follow", false, "keep streaming until interrupted")
	if err := fs.Parse(intersperse(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: pici-cli logs <execution-id> [--follow]")
	}
	id := fs.Arg(0)
	offset := 0
	for {
		logs, err := c.ExecutionLogs(context.Background(), id)
		if err != nil {
			return err
		}
		if len(logs) > offset {
			fmt.Print(logs[offset:])
			offset = len(logs)
		}
		if !*follow {
			return nil
		}
		time.Sleep(time.Second)
	}
}

func cmdProjects(c *client.Client, args []string) error {
	if len(args) == 0 {
		projects, err := c.ListProjects(context.Background())
		if err != nil {
			return err
		}
		return output(projects)
	}
	switch args[0] {
	case "add":
		return cmdProjectAdd(c, args[1:])
	case "show":
		return cmdProjectShow(c, args[1:])
	case "update":
		return cmdProjectUpdate(c, args[1:])
	case "rm":
		return cmdProjectRm(c, args[1:])
	default:
		return fmt.Errorf("unknown projects subcommand %q (add, show, update, rm)", args[0])
	}
}

func cmdProjectAdd(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("projects add", flag.ExitOnError)
	provider := fs.String("provider", "", "github, gitlab, or generic")
	authType := fs.String("auth-type", "", "none, token, or ssh")
	authUser := fs.String("auth-user", "", "")
	authSecret := fs.String("auth-secret", "", "")
	webhookSecret := fs.String("webhook-secret", "", "")
	defaultBranch := fs.String("default-branch", "", "")
	if err := fs.Parse(intersperse(args)); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: pici-cli projects add <name> <repo_url> [flags]")
	}
	p, err := c.CreateProject(context.Background(), client.CreateProjectRequest{
		Name:          fs.Arg(0),
		RepoURL:       fs.Arg(1),
		Provider:      client.Provider(*provider),
		AuthType:      client.AuthType(*authType),
		AuthUser:      *authUser,
		AuthSecret:    *authSecret,
		WebhookSecret: *webhookSecret,
		DefaultBranch: *defaultBranch,
	})
	if err != nil {
		return err
	}
	return output(p)
}

func cmdProjectShow(c *client.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: pici-cli projects show <name-or-id>")
	}
	p, err := c.GetProject(context.Background(), args[0])
	if err != nil {
		return err
	}
	return output(p)
}

func cmdProjectUpdate(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("projects update", flag.ExitOnError)
	name := fs.String("name", "", "")
	repoURL := fs.String("repo-url", "", "")
	provider := fs.String("provider", "", "github, gitlab, or generic")
	authType := fs.String("auth-type", "", "none, token, or ssh")
	authUser := fs.String("auth-user", "", "")
	authSecret := fs.String("auth-secret", "", "")
	webhookSecret := fs.String("webhook-secret", "", "")
	defaultBranch := fs.String("default-branch", "", "")
	if err := fs.Parse(intersperse(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pici-cli projects update <name-or-id> [flags]")
	}
	p, err := c.UpdateProject(context.Background(), fs.Arg(0), client.UpdateProjectRequest{
		Name:          *name,
		RepoURL:       *repoURL,
		Provider:      client.Provider(*provider),
		AuthType:      client.AuthType(*authType),
		AuthUser:      *authUser,
		AuthSecret:    *authSecret,
		WebhookSecret: *webhookSecret,
		DefaultBranch: *defaultBranch,
	})
	if err != nil {
		return err
	}
	return output(p)
}

func cmdProjectRm(c *client.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: pici-cli projects rm <name-or-id>")
	}
	return c.DeleteProject(context.Background(), args[0])
}

func cmdExecutions(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("executions", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max results")
	if err := fs.Parse(intersperse(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: pici-cli executions <project> [--limit N]")
	}
	executions, err := c.ListExecutions(context.Background(), fs.Arg(0), *limit)
	if err != nil {
		return err
	}
	return output(executions)
}

func cmdVars(c *client.Client, args []string) error {
	var project string
	var secret bool
	var pos []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			i++
			if i >= len(args) {
				return fmt.Errorf("--project requires a value")
			}
			project = args[i]
		case "--secret":
			secret = true
		default:
			pos = append(pos, args[i])
		}
	}

	switch {
	case len(pos) == 0:
		var vars []client.Variable
		var err error
		if project == "" {
			vars, err = c.ListGlobalVariables(context.Background())
		} else {
			vars, err = c.ListProjectVariables(context.Background(), project)
		}
		if err != nil {
			return err
		}
		return output(vars)
	case pos[0] == "set" && len(pos) == 3:
		if project == "" {
			return c.SetGlobalVariable(context.Background(), pos[1], pos[2], secret)
		}
		return c.SetProjectVariable(context.Background(), project, pos[1], pos[2], secret)
	case pos[0] == "rm" && len(pos) == 2:
		if project == "" {
			return c.DeleteGlobalVariable(context.Background(), pos[1])
		}
		return c.DeleteProjectVariable(context.Background(), project, pos[1])
	default:
		return fmt.Errorf("usage: pici-cli vars [--project <name>] [set <key> <value> [--secret] | rm <key>]")
	}
}

func cmdCancel(c *client.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: pici-cli cancel <execution-id>")
	}
	return c.CancelExecution(context.Background(), args[0])
}

func cmdRebuild(c *client.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: pici-cli rebuild <execution-id>")
	}
	exec, err := c.RebuildExecution(context.Background(), args[0])
	if err != nil {
		return err
	}
	return output(exec)
}

func cmdArtifacts(c *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pici-cli artifacts <execution-id> | pici-cli artifacts get <execution-id> <step>/<path>")
	}
	if args[0] == "get" {
		return cmdArtifactGet(c, args[1:])
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: pici-cli artifacts <execution-id>")
	}
	artifacts, err := c.ListArtifacts(context.Background(), args[0])
	if err != nil {
		return err
	}
	return output(artifacts)
}

func cmdArtifactGet(c *client.Client, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: pici-cli artifacts get <execution-id> <step>/<path>")
	}
	step, path, ok := strings.Cut(args[1], "/")
	if !ok {
		return fmt.Errorf("artifact path must be <step>/<path>")
	}
	data, err := c.DownloadArtifact(context.Background(), args[0], step, path)
	if err != nil {
		return err
	}
	return os.WriteFile(path[strings.LastIndex(path, "/")+1:], data, 0o644)
}

func cmdValidate(c *client.Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pici-cli validate <ci.yml>")
	}
	content, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	out, err := c.Validate(context.Background(), string(content))
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// intersperse moves flags (and their values) before positional arguments,
// since stdlib flag stops parsing at the first non-flag argument.
func intersperse(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			pos = append(pos, args[i])
			continue
		}
		flags = append(flags, args[i])
		if !strings.Contains(args[i], "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, pos...)
}
