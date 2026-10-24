package ci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/docker"
)

// runningSandbox is the throwaway daemon of an execution and the client pici
// runs its step containers through.
type runningSandbox struct {
	sandbox docker.Sandbox
	engine  docker.Daemon
}

// workflowNeedsSandbox reports whether a workflow declared any step that needs
// Docker. One such step puts the whole execution in a sandbox, so the services
// it starts stay reachable by the following steps.
func workflowNeedsSandbox(cfg Config) bool {
	for _, step := range cfg.Steps {
		if step.Docker {
			return true
		}
	}
	return false
}

// registryReference is the workflow image ref used inside the sandbox, or an
// empty string when the image registry is unavailable.
func registryReference(registries docker.Registries, projectID, workflow string) string {
	if registries.Images == "" {
		return ""
	}
	return registries.Images + "/" + imageTag(projectID, workflow)
}

func (r *Runner) startSandbox(ctx context.Context, registries docker.Registries, projectID uuid.UUID, execID int64, repoDir string, caches []string, image string, logs io.Writer) (*runningSandbox, error) {
	if registries.Images == "" {
		return nil, errors.New("image registry is not running")
	}
	fmt.Fprintf(logs, "starting docker sandbox for %s...\n", image)
	sandbox, err := r.Engine.CreateSandbox(ctx, docker.SandboxOptions{
		Image:     r.SandboxImage,
		RepoDir:   repoDir,
		MountPath: r.MountPath,
		Caches:    sandboxCaches(caches),
		Registry:  registries.Images,
		Mirror:    registries.HubCache,
		Addr:      r.SandboxAddr,
		Labels: map[string]string{
			docker.LabelSandbox: "1",
			docker.LabelProject: projectID.String(),
			docker.LabelAttempt: strconv.FormatInt(execID, 10),
		},
	})
	if err != nil {
		return nil, err
	}
	engine, err := r.Engine.Dial(sandbox.Host)
	if err != nil {
		_ = r.Engine.RemoveSandbox(context.Background(), sandbox.ID)
		return nil, err
	}
	if err := engine.Pull(ctx, image, logs); err != nil {
		_ = engine.Close()
		_ = r.Engine.RemoveSandbox(context.Background(), sandbox.ID)
		return nil, err
	}
	return &runningSandbox{sandbox: sandbox, engine: engine}, nil
}

// stopSandbox removes the sandbox in a detached context so a canceled or timed
// out execution still cleans up.
func (r *Runner) stopSandbox(sb *runningSandbox) {
	_ = sb.engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := r.Engine.RemoveSandbox(ctx, sb.sandbox.ID); err != nil {
		slog.Warn("remove sandbox", "id", sb.sandbox.ID, "error", err)
	}
}

// sandboxCaches turns the step cache binds into mounts for the sandbox itself.
func sandboxCaches(caches []string) []docker.CacheMount {
	out := make([]docker.CacheMount, 0, len(caches))
	for _, c := range caches {
		if volume, target, ok := strings.Cut(c, ":"); ok {
			out = append(out, docker.CacheMount{Volume: volume, Target: target})
		}
	}
	return out
}

// sandboxStepBinds re-points the step mounts at the sandbox's own filesystem:
// the repo on the outer daemon becomes a plain bind of the same path, and each
// cache volume is already mounted there.
func sandboxStepBinds(mountPath string, caches []string) []string {
	binds := make([]string, 0, len(caches)+1)
	binds = append(binds, mountPath+":"+mountPath)
	for _, c := range caches {
		if _, target, ok := strings.Cut(c, ":"); ok {
			binds = append(binds, target+":"+target)
		}
	}
	return binds
}
