package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Thiht/pici/client"
	"github.com/Thiht/pici/internal/version"
)

var buildVersion = "dev"

func main() {
	c := client.New(cmp.Or(os.Getenv("PICI_ADDR"), "http://localhost:8080"), os.Getenv("PICI_TOKEN"))
	if err := newRootCmd(c).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd(c *client.Client) *cobra.Command {
	root := &cobra.Command{
		Use:           "pici-cli",
		Short:         "pici — minimal CI client",
		Long:          usageText,
		Version:       buildVersion,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.PersistentFlags().Bool("json", false, "force JSON output")
	root.PersistentFlags().String("format", "", "force output format (json|pretty)")
	_ = root.RegisterFlagCompletionFunc("format", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "pretty"}, cobra.ShellCompDirectiveNoFileComp
	})

	root.AddCommand(
		newRunCmd(c),
		newLogsCmd(c),
		newStatusCmd(c),
		newProjectsCmd(c),
		newExecutionsCmd(c),
		newVarsCmd(c),
		newCacheCmd(c),
		newCancelCmd(c),
		newRebuildCmd(c),
		newArtifactsCmd(c),
		newValidateCmd(c),
		newHealthCmd(c),
		newVersionCmd(c),
	)
	return root
}

func newRunCmd(c *client.Client) *cobra.Command {
	var ref string
	var local bool
	var dir string
	cmd := &cobra.Command{
		Use:   "run <project> <workflow>",
		Short: "Trigger a workflow execution",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !local {
				exec, err := c.TriggerExecution(context.Background(), args[0], args[1], ref)
				if err != nil {
					return err
				}
				fmt.Println(exec.ID)
				return nil
			}

			workdir := dir
			if workdir == "" {
				var err error
				if workdir, err = os.Getwd(); err != nil {
					return err
				}
			}
			localRef, sha, err := worktreeInfo(workdir)
			if err != nil {
				return err
			}
			if ref != "" {
				localRef = ref
			}

			pr, pw := io.Pipe()
			go func() {
				err := buildSnapshot(workdir, pw)
				pw.CloseWithError(err)
			}()

			exec, err := c.TriggerSnapshotExecution(context.Background(), args[0], args[1], localRef, sha, pr)
			if err != nil {
				return err
			}
			fmt.Println(exec.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "git ref to build")
	cmd.Flags().BoolVar(&local, "local", false, "run on the current worktree, including uncommitted changes")
	cmd.Flags().StringVar(&dir, "dir", "", "worktree directory to snapshot (default: current directory)")
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return projectNames(c), cobra.ShellCompDirectiveNoFileComp
		}
		workflows, err := c.ListWorkflows(context.Background(), args[0])
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return workflows, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newLogsCmd(c *client.Client) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <project> <execution-id>",
		Short: "Stream execution logs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return err
			}
			offset := 0
			for {
				logs, err := c.ExecutionLogs(context.Background(), args[0], id)
				if err != nil {
					return err
				}
				if len(logs) > offset {
					fmt.Print(logs[offset:])
					offset = len(logs)
				}
				if !follow {
					return nil
				}
				time.Sleep(time.Second)
			}
		},
	}
	cmd.Flags().BoolVar(&follow, "follow", false, "keep streaming until interrupted")
	cmd.ValidArgsFunction = noCompletions
	return cmd
}

func newStatusCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <project> <execution-id>",
		Short: "Show execution status",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return err
			}
			exec, err := c.GetExecution(context.Background(), args[0], id)
			if err != nil {
				return err
			}
			return output(exec)
		},
	}
	cmd.ValidArgsFunction = noCompletions
	return cmd
}

func newProjectsCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			projects, err := c.ListProjects(context.Background())
			if err != nil {
				return err
			}
			return output(projects)
		},
	}
	cmd.AddCommand(newProjectAddCmd(c), newProjectShowCmd(c), newProjectUpdateCmd(c), newProjectRmCmd(c))
	return cmd
}

func newProjectAddCmd(c *client.Client) *cobra.Command {
	var provider, authType, authUser, authSecret, webhookSecret, defaultBranch string
	cmd := &cobra.Command{
		Use:   "add <name> <repo-url>",
		Short: "Add a project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			p, err := c.CreateProject(context.Background(), client.CreateProjectRequest{
				Name:          args[0],
				RepoURL:       args[1],
				Provider:      client.Provider(provider),
				AuthType:      client.AuthType(authType),
				AuthUser:      authUser,
				AuthSecret:    authSecret,
				WebhookSecret: webhookSecret,
				DefaultBranch: defaultBranch,
			})
			if err != nil {
				return err
			}
			return output(p)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "github, gitlab, or generic")
	cmd.Flags().StringVar(&authType, "auth-type", "", "none, token, or ssh")
	cmd.Flags().StringVar(&authUser, "auth-user", "", "")
	cmd.Flags().StringVar(&authSecret, "auth-secret", "", "")
	cmd.Flags().StringVar(&webhookSecret, "webhook-secret", "", "")
	cmd.Flags().StringVar(&defaultBranch, "default-branch", "", "")
	_ = cmd.RegisterFlagCompletionFunc("provider", enumCompletions("github", "gitlab", "generic"))
	_ = cmd.RegisterFlagCompletionFunc("auth-type", enumCompletions("none", "token", "ssh"))
	return cmd
}

func newProjectShowCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name-or-id>",
		Short: "Show a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			p, err := c.GetProject(context.Background(), args[0])
			if err != nil {
				return err
			}
			return output(p)
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return projectNames(c), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newProjectUpdateCmd(c *client.Client) *cobra.Command {
	var name, repoURL, provider, authType, authUser, authSecret, webhookSecret, defaultBranch string
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			p, err := c.UpdateProject(context.Background(), args[0], client.UpdateProjectRequest{
				Name:          name,
				RepoURL:       repoURL,
				Provider:      client.Provider(provider),
				AuthType:      client.AuthType(authType),
				AuthUser:      authUser,
				AuthSecret:    authSecret,
				WebhookSecret: webhookSecret,
				DefaultBranch: defaultBranch,
			})
			if err != nil {
				return err
			}
			return output(p)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "")
	cmd.Flags().StringVar(&repoURL, "repo-url", "", "")
	cmd.Flags().StringVar(&provider, "provider", "", "github, gitlab, or generic")
	cmd.Flags().StringVar(&authType, "auth-type", "", "none, token, or ssh")
	cmd.Flags().StringVar(&authUser, "auth-user", "", "")
	cmd.Flags().StringVar(&authSecret, "auth-secret", "", "")
	cmd.Flags().StringVar(&webhookSecret, "webhook-secret", "", "")
	cmd.Flags().StringVar(&defaultBranch, "default-branch", "", "")
	_ = cmd.RegisterFlagCompletionFunc("provider", enumCompletions("github", "gitlab", "generic"))
	_ = cmd.RegisterFlagCompletionFunc("auth-type", enumCompletions("none", "token", "ssh"))
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return projectNames(c), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newProjectRmCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <name-or-id>",
		Short: "Remove a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.DeleteProject(context.Background(), args[0])
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return projectNames(c), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newExecutionsCmd(c *client.Client) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "executions <project>",
		Short: "List executions for a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			executions, err := c.ListExecutions(context.Background(), args[0], limit)
			if err != nil {
				return err
			}
			return output(executions)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return projectNames(c), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newVarsCmd(c *client.Client) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "vars",
		Short: "List or manage variables",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			if project == "" {
				vars, err := c.ListGlobalVariables(context.Background())
				if err != nil {
					return err
				}
				return output(vars)
			}
			vars, err := c.ListProjectVariables(context.Background(), project)
			if err != nil {
				return err
			}
			return output(vars)
		},
	}
	cmd.PersistentFlags().StringVar(&project, "project", "", "scope variables to a project")
	_ = cmd.RegisterFlagCompletionFunc("project", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return projectNames(c), cobra.ShellCompDirectiveNoFileComp
	})
	cmd.AddCommand(newVarSetCmd(c, &project), newVarRmCmd(c, &project))
	return cmd
}

func newVarSetCmd(c *client.Client, project *string) *cobra.Command {
	var secret bool
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a variable",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if *project == "" {
				return c.SetGlobalVariable(context.Background(), args[0], args[1], secret)
			}
			return c.SetProjectVariable(context.Background(), *project, args[0], args[1], secret)
		},
	}
	cmd.Flags().BoolVar(&secret, "secret", false, "mark the value as secret")
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return variableKeys(c, *project), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newVarRmCmd(c *client.Client, project *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <key>",
		Short: "Remove a variable",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if *project == "" {
				return c.DeleteGlobalVariable(context.Background(), args[0])
			}
			return c.DeleteProjectVariable(context.Background(), *project, args[0])
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return variableKeys(c, *project), cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newCancelCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel <project> <execution-id>",
		Short: "Cancel an execution",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return err
			}
			return c.CancelExecution(context.Background(), args[0], id)
		},
	}
	cmd.ValidArgsFunction = noCompletions
	return cmd
}

func newRebuildCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild <project> <execution-id>",
		Short: "Rebuild an execution",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return err
			}
			exec, err := c.RebuildExecution(context.Background(), args[0], id)
			if err != nil {
				return err
			}
			return output(exec)
		},
	}
	cmd.ValidArgsFunction = noCompletions
	return cmd
}

func newArtifactsCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifacts <project> <execution-id>",
		Short: "List execution artifacts",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return err
			}
			artifacts, err := c.ListArtifacts(context.Background(), args[0], id)
			if err != nil {
				return err
			}
			return output(artifacts)
		},
	}
	cmd.ValidArgsFunction = noCompletions
	cmd.AddCommand(newArtifactGetCmd(c))
	return cmd
}

func newArtifactGetCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <project> <execution-id> <step>/<path>",
		Short: "Download an artifact",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return err
			}
			step, path, ok := strings.Cut(args[2], "/")
			if !ok {
				return fmt.Errorf("artifact path must be <step>/<path>")
			}
			data, err := c.DownloadArtifact(context.Background(), args[0], id, step, path)
			if err != nil {
				return err
			}
			return os.WriteFile(path[strings.LastIndex(path, "/")+1:], data, 0o644)
		},
	}
	cmd.ValidArgsFunction = noCompletions
	return cmd
}

func newValidateCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <ci.yml>",
		Short: "Validate a CI config file",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yml", "yaml"}, cobra.ShellCompDirectiveFilterFileExt
	}
	return cmd
}

func newHealthCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check server health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			h, err := c.Health(context.Background())
			if err != nil {
				return err
			}
			return output(h)
		},
	}
	return cmd
}

func newVersionCmd(c *client.Client) *cobra.Command {
	var server bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			if server {
				v, err := c.Version(context.Background())
				if err != nil {
					return err
				}
				return output(v)
			}
			return output(version.Get(buildVersion))
		},
	}
	cmd.Flags().BoolVar(&server, "server", false, "show the server version instead of the local build")
	return cmd
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func applyFormat(cmd *cobra.Command) error {
	if v, _ := cmd.Flags().GetBool("json"); v {
		formatFlag = formatJSON
		return nil
	}
	if s, _ := cmd.Flags().GetString("format"); s != "" {
		return setFormat(s)
	}
	formatFlag = formatAuto
	return nil
}

func projectNames(c *client.Client) []string {
	projects, err := c.ListProjects(context.Background())
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}
	return names
}

func variableKeys(c *client.Client, project string) []string {
	var vars []client.Variable
	var err error
	if project == "" {
		vars, err = c.ListGlobalVariables(context.Background())
	} else {
		vars, err = c.ListProjectVariables(context.Background(), project)
	}
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(vars))
	for _, v := range vars {
		keys = append(keys, v.Key)
	}
	return keys
}

func enumCompletions(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

func noCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

const usageText = `pici — minimal CI client

Usage:
  pici-cli run <project> <workflow> [--ref <ref>] [--local [--dir <path>]]
  pici-cli logs <project> <execution-id> [--follow]
  pici-cli status <project> <execution-id>
  pici-cli projects
  pici-cli projects add <name> <repo_url> [flags]
  pici-cli projects show <name-or-id>
  pici-cli projects update <name-or-id> [flags]
  pici-cli projects rm <name-or-id>
  pici-cli executions <project> [--limit N]
  pici-cli vars [--project <name>]
  pici-cli vars set <key> <value> [--project <name>] [--secret]
  pici-cli vars rm <key> [--project <name>]
  pici-cli cache <project>
  pici-cli cache rm-image <project> <reference>
  pici-cli cache rm-volume <project> <name>
  pici-cli cancel <project> <execution-id>
  pici-cli rebuild <project> <execution-id>
  pici-cli artifacts <project> <execution-id>
  pici-cli artifacts get <project> <execution-id> <step>/<path>
  pici-cli validate <ci.yml>
  pici-cli health
  pici-cli version [--server]
  pici-cli completion [bash|zsh|fish|powershell]

Global flags:
  --json             force JSON output
  --format json|pretty   force output format (default: pretty on a TTY, JSON otherwise)

Project flags (add/update):
  --provider X  --auth-type X  --auth-user X  --auth-secret X
  --webhook-secret X  --default-branch X  (update: also --name, --repo-url)

Env:
  PICI_ADDR   server base URL (default http://localhost:8080)
  PICI_TOKEN  API token (sent as Authorization: Bearer)
`
