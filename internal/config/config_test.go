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
