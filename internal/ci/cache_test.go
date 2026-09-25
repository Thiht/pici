package ci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCachesGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, paths := detectCaches(dir, "/workspace")
	if env["GOMODCACHE"] != "/workspace/.cache/gomod" {
		t.Fatalf("expected GOMODCACHE, got %q", env["GOMODCACHE"])
	}
	if env["GOCACHE"] != "/workspace/.cache/gobuild" {
		t.Fatalf("expected GOCACHE, got %q", env["GOCACHE"])
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 cache paths, got %v", paths)
	}
}

func TestDetectCachesNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, paths := detectCaches(dir, "/workspace")
	if env["npm_config_cache"] != "/workspace/.npm" {
		t.Fatalf("expected npm_config_cache, got %q", env["npm_config_cache"])
	}
	if len(paths) != 1 || paths[0] != ".npm" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestDetectCachesNone(t *testing.T) {
	env, paths := detectCaches(t.TempDir(), "/workspace")
	if len(env) != 0 || len(paths) != 0 {
		t.Fatalf("expected no caches, got env=%v paths=%v", env, paths)
	}
}
