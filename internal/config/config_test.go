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
	if err := (Config{SecretKey: "x", APIToken: "y", MaxSnapshotSize: 1}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateInvalidMaxSnapshotSize(t *testing.T) {
	for _, size := range []int64{0, -1} {
		cfg := Config{SecretKey: "x", APIToken: "y", MaxSnapshotSize: size}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for max snapshot size %d", size)
		}
	}
}

func TestValidateRepoMountPathOverlapsCache(t *testing.T) {
	for _, path := range []string{"/", "/pici", "/pici/cache", "/pici/cache/nested"} {
		cfg := Config{SecretKey: "x", APIToken: "y", MaxSnapshotSize: 1, RepoMountPath: path}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for repo mount path %q", path)
		}
	}
}

func TestValidateRepoMountPathOK(t *testing.T) {
	for _, path := range []string{"", "/workspace", "/workspace/nested", "/pici2"} {
		cfg := Config{SecretKey: "x", APIToken: "y", MaxSnapshotSize: 1, RepoMountPath: path}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error for repo mount path %q: %v", path, err)
		}
	}
}
