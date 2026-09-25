package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Thiht/pici/client"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	c := client.New(cmp.Or(os.Getenv("PICI_ADDR"), "http://localhost:8080"), os.Getenv("PICI_TOKEN"))

	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(c, os.Args[2:])
	case "logs":
		err = cmdLogs(c, os.Args[2:])
	case "status":
		err = cmdStatus(c, os.Args[2:])
	case "projects":
		err = cmdProjects(c, os.Args[2:])
	case "executions":
		err = cmdExecutions(c, os.Args[2:])
	case "validate":
		err = cmdValidate(c, os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
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
  pici-cli executions <project> [--limit N]
  pici-cli validate <ci.yml>

Env:
  PICI_ADDR   server base URL (default http://localhost:8080)
  PICI_TOKEN  API token (sent as Authorization: Bearer)
`)
}

func cmdRun(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	ref := fs.String("ref", "", "git ref to build")
	if err := fs.Parse(args); err != nil {
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
	return printJSON(exec)
}

func cmdLogs(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("follow", false, "keep streaming until interrupted")
	if err := fs.Parse(args); err != nil {
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

func cmdProjects(c *client.Client, _ []string) error {
	projects, err := c.ListProjects(context.Background())
	if err != nil {
		return err
	}
	return printJSON(projects)
}

func cmdExecutions(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("executions", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: pici-cli executions <project> [--limit N]")
	}
	executions, err := c.ListExecutions(context.Background(), fs.Arg(0), *limit)
	if err != nil {
		return err
	}
	return printJSON(executions)
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
