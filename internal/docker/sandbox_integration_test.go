//go:build docker

package docker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests exercise the throwaway daemon against a real Docker daemon. Run
// them with `go test -tags=docker ./internal/docker`. They leave pici's
// registries running, exactly like a pici instance would.

func testEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Ping(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	return engine
}

func TestSandboxLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	engine := testEngine(t)

	registries, err := engine.EnsureRegistries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RegistryUsage(ctx); err != nil {
		t.Fatalf("registry usage: %v", err)
	}

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("hi from the repo"), 0o644); err != nil {
		t.Fatal(err)
	}

	labels := map[string]string{LabelSandbox: "1", LabelProject: "test", LabelAttempt: "1"}
	sandbox, err := engine.CreateSandbox(ctx, SandboxOptions{
		RepoDir:   repo,
		MountPath: "/workspace",
		Registry:  registries.Images,
		Mirror:    registries.HubCache,
		Labels:    labels,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = engine.RemoveSandbox(removeCtx, sandbox.ID)
	})

	// A second sandbox carrying the same attempt replaces the first one, which
	// is what a requeued execution relies on.
	replacement, err := engine.CreateSandbox(ctx, SandboxOptions{
		RepoDir: repo, MountPath: "/workspace", Registry: registries.Images, Mirror: registries.HubCache, Labels: labels,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = engine.RemoveSandbox(removeCtx, replacement.ID)
	})
	if sandbox.ID == replacement.ID {
		t.Fatal("expected a new sandbox")
	}
	sandboxes, err := engine.Sandboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes) != 1 || sandboxes[0].ID != replacement.ID {
		t.Fatalf("expected only the replacement sandbox, got %+v", sandboxes)
	}

	sandboxed, err := engine.Dial(replacement.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandboxed.Close() }()

	// The pull goes through the Docker Hub mirror configured on the sandbox.
	if err := sandboxed.Pull(ctx, "busybox:latest", nil); err != nil {
		t.Fatalf("pull through the sandbox: %v", err)
	}

	var logs bytes.Buffer
	code, err := sandboxed.Run(ctx, RunOptions{
		Image:      "busybox:latest",
		Command:    []string{"sh", "-c", "cat /workspace/hello.txt && echo done"},
		Binds:      []string{"/workspace:/workspace"},
		WorkingDir: "/workspace",
		Logs:       &logs,
		Timeout:    time.Minute,
	})
	if err != nil || code != 0 {
		t.Fatalf("sandboxed run: code=%d err=%v logs=%s", code, err, logString(&logs))
	}
	if !strings.Contains(logs.String(), "hi from the repo") {
		t.Fatalf("the repo was not mounted in the sandbox: %q", logs.String())
	}

	removeCtx, removeCancel := context.WithTimeout(context.Background(), time.Minute)
	defer removeCancel()
	if err := engine.RemoveSandbox(removeCtx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	gone, err := NewEngineFor(replacement.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gone.Close() }()
	if err := gone.Ping(removeCtx); err == nil {
		t.Fatal("the sandbox daemon should be gone")
	}
}

// TestRegistryPushAndPull pushes an image to the internal registry and pulls it
// from another sandbox. The push happens from a sandbox because it is the only
// daemon in this test that trusts the internal registry; pici's own daemon gets
// that trust from docker/docker-entrypoint.sh, and an external daemon must be
// started with --insecure-registry.
func TestRegistryPushAndPull(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	engine := testEngine(t)

	registries, err := engine.EnsureRegistries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	labels := func(attempt string) map[string]string {
		return map[string]string{LabelSandbox: "1", LabelProject: "test", LabelAttempt: attempt}
	}
	newSandbox := func(attempt string) *Engine {
		t.Helper()
		sandbox, err := engine.CreateSandbox(ctx, SandboxOptions{
			RepoDir:   t.TempDir(),
			MountPath: "/workspace",
			Registry:  registries.Images,
			Mirror:    registries.HubCache,
			Labels:    labels(attempt),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			removeCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = engine.RemoveSandbox(removeCtx, sandbox.ID)
		})
		daemon, err := NewEngineFor(sandbox.Host)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = daemon.Close() })
		return daemon
	}

	pusher := newSandbox("2")
	if err := pusher.Pull(ctx, "busybox:latest", nil); err != nil {
		t.Fatal(err)
	}
	ref := registries.Images + "/pici/sandbox-test:latest"
	if err := pusher.cli.ImageTag(ctx, "busybox:latest", ref); err != nil {
		t.Fatal(err)
	}
	if err := pusher.Push(ctx, ref, nil); err != nil {
		t.Fatalf("push to the image registry: %v", err)
	}

	puller := newSandbox("3")
	if err := puller.Pull(ctx, ref, nil); err != nil {
		t.Fatalf("pull from the image registry: %v", err)
	}
}

func logString(buf *bytes.Buffer) string {
	if buf.Len() > 4096 {
		return buf.String()[:4096] + "..."
	}
	return buf.String()
}
