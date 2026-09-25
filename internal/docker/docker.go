package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
)

type Engine struct {
	cli *client.Client
}

func New() (*Engine, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Engine{cli: cli}, nil
}

func (e *Engine) Close() error { return e.cli.Close() }

func (e *Engine) Ping(ctx context.Context) error {
	_, err := e.cli.Ping(ctx)
	return err
}

func (e *Engine) PruneImages(ctx context.Context) error {
	f := filters.NewArgs(filters.Arg("label", "pici=1"))
	_, err := e.cli.ImagesPrune(ctx, f)
	return err
}

type BuildOptions struct {
	ContextDir string
	Dockerfile string
	Tags       []string
	CacheFrom  []string
	Progress   io.Writer
}

func (e *Engine) Build(ctx context.Context, opts BuildOptions) error {
	tarReader, err := tarContext(opts.ContextDir)
	if err != nil {
		return err
	}

	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}

	resp, err := e.cli.ImageBuild(ctx, tarReader, build.ImageBuildOptions{
		Dockerfile: opts.Dockerfile,
		Tags:       opts.Tags,
		CacheFrom:  opts.CacheFrom,
		PullParent: true,
		Remove:     true,
		Labels:     map[string]string{"pici": "1"},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := jsonmessage.DisplayJSONMessagesStream(resp.Body, progress, 0, false, nil); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	return nil
}

type RunOptions struct {
	Image      string
	Command    []string
	Env        []string
	Binds      []string
	WorkingDir string
	Logs       io.Writer
	Timeout    time.Duration
}

func (e *Engine) Run(ctx context.Context, opts RunOptions) (int, error) {
	runCtx := ctx
	cancel := func() {}
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}
	defer cancel()

	cfg := &container.Config{
		Image:      opts.Image,
		Cmd:        opts.Command,
		Env:        opts.Env,
		WorkingDir: opts.WorkingDir,
	}
	hostCfg := &container.HostConfig{
		Binds: opts.Binds,
	}

	created, err := e.cli.ContainerCreate(runCtx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return -1, err
	}
	id := created.ID
	defer func() { _ = e.cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true}) }()

	if err := e.cli.ContainerStart(runCtx, id, container.StartOptions{}); err != nil {
		return -1, err
	}

	logs := opts.Logs
	if logs == nil {
		logs = io.Discard
	}
	logStream, err := e.cli.ContainerLogs(runCtx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return -1, err
	}
	logDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(logs, logs, logStream)
		logStream.Close()
		logDone <- err
	}()

	waitCh, errCh := e.cli.ContainerWait(runCtx, id, container.WaitConditionNotRunning)
	var code int
	select {
	case status := <-waitCh:
		code = int(status.StatusCode)
	case err := <-errCh:
		if err != nil {
			return -1, err
		}
	}
	<-logDone
	return code, nil
}

func tarContext(dir string) (io.Reader, error) {
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		defer tw.Close()
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)

			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = rel
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				if _, err := io.Copy(tw, f); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()
	return pr, nil
}

func CleanTags(tag string) string {
	return strings.ReplaceAll(tag, "/", "_")
}
