package secrets

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32))
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := "super-secret-value"
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, prefix) {
		t.Fatalf("expected prefix %q, got %q", prefix, enc)
	}
	if enc == plain {
		t.Fatal("ciphertext equals plaintext")
	}
	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != plain {
		t.Fatalf("got %q, want %q", dec, plain)
	}
}

func TestDecryptPlaintextPassthrough(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32))
	c, _ := New(key)
	got, err := c.Decrypt("not-encrypted")
	if err != nil {
		t.Fatal(err)
	}
	if got != "not-encrypted" {
		t.Fatalf("got %q", got)
	}
}

func TestInvalidKey(t *testing.T) {
	if _, err := New("short"); err == nil {
		t.Fatal("expected error for short key")
	}
}
