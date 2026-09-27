package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectCachesGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, paths := detectCaches(dir, CacheMountPath)
	if env["GOMODCACHE"] != filepath.Join(CacheMountPath, "gomod") {
		t.Fatalf("expected GOMODCACHE, got %q", env["GOMODCACHE"])
	}
	if env["GOCACHE"] != filepath.Join(CacheMountPath, "gobuild") {
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

	env, paths := detectCaches(dir, CacheMountPath)
	if env["npm_config_cache"] != filepath.Join(CacheMountPath, "npm") {
		t.Fatalf("expected npm_config_cache, got %q", env["npm_config_cache"])
	}
	if len(paths) != 1 || paths[0] != "npm" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestDetectCachesMaven(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, paths := detectCaches(dir, CacheMountPath)
	want := "-Dmaven.repo.local=" + filepath.Join(CacheMountPath, "m2")
	if env["MAVEN_OPTS"] != want {
		t.Fatalf("expected MAVEN_OPTS %q, got %q", want, env["MAVEN_OPTS"])
	}
	if len(paths) != 1 || paths[0] != "m2" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestDetectCachesTerraformGlob(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	env, paths := detectCaches(dir, CacheMountPath)
	if env["TF_PLUGIN_CACHE_DIR"] != filepath.Join(CacheMountPath, "terraform") {
		t.Fatalf("expected TF_PLUGIN_CACHE_DIR, got %q", env["TF_PLUGIN_CACHE_DIR"])
	}
	if len(paths) != 1 || paths[0] != "terraform" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestDetectCachesNone(t *testing.T) {
	env, paths := detectCaches(t.TempDir(), CacheMountPath)
	if len(env) != 0 || len(paths) != 0 {
		t.Fatalf("expected no caches, got env=%v paths=%v", env, paths)
	}
}

func TestAutoCacheBindsOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, paths := detectCaches(dir, CacheMountPath)
	binds := cacheBinds("proj", CacheMountPath, paths)
	if len(binds) != 2 {
		t.Fatalf("expected 2 binds, got %v", binds)
	}
	for _, b := range binds {
		container := strings.SplitN(b, ":", 2)[1]
		if !strings.HasPrefix(container, CacheMountPath+"/") {
			t.Fatalf("auto cache %q is not under %s", container, CacheMountPath)
		}
	}
}
