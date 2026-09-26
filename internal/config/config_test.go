package config

import "testing"

func TestValidateMissingSecretKey(t *testing.T) {
	if err := (Config{APIToken: "x"}).Validate(); err == nil {
		t.Fatal("expected error for missing secret key")
	}
}

func TestValidateMissingAPIToken(t *testing.T) {
	if err := (Config{SecretKey: "x"}).Validate(); err == nil {
		t.Fatal("expected error for missing API token")
	}
}

func TestValidateOK(t *testing.T) {
	if err := (Config{SecretKey: "x", APIToken: "y"}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoMountPathOverlapsCache(t *testing.T) {
	for _, path := range []string{"/", "/pici", "/pici/cache", "/pici/cache/nested"} {
		cfg := Config{SecretKey: "x", APIToken: "y", RepoMountPath: path}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for repo mount path %q", path)
		}
	}
}

func TestValidateRepoMountPathOK(t *testing.T) {
	for _, path := range []string{"", "/workspace", "/workspace/nested", "/pici2"} {
		cfg := Config{SecretKey: "x", APIToken: "y", RepoMountPath: path}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error for repo mount path %q: %v", path, err)
		}
	}
}
