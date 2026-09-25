package mask

import (
	"bytes"
	"testing"
)

func TestRedact(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, []string{"supersecret", "token123"})
	_, _ = w.Write([]byte("echo supersecret here\n"))
	_, _ = w.Write([]byte("token123 leaked"))
	_ = w.Flush()

	out := buf.String()
	if bytes.Contains([]byte(out), []byte("supersecret")) {
		t.Fatalf("secret not redacted: %q", out)
	}
	if bytes.Contains([]byte(out), []byte("token123")) {
		t.Fatalf("secret not redacted: %q", out)
	}
	if !bytes.Contains([]byte(out), []byte("***")) {
		t.Fatalf("expected redaction marker: %q", out)
	}
}

func TestRedactAcrossChunks(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, []string{"supersecret"})
	_, _ = w.Write([]byte("echo super"))
	_, _ = w.Write([]byte("secret\n"))
	_ = w.Flush()
	if bytes.Contains(buf.Bytes(), []byte("supersecret")) {
		t.Fatalf("cross-chunk secret not redacted: %q", buf.String())
	}
}
