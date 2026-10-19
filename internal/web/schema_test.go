package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Thiht/pici/internal/ci"
)

func TestSchemaRoute(t *testing.T) {
	handler := New(nil, nil, "", nil, "test").Routes()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/ci.schema.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (no session required), got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/schema+json" {
		t.Errorf("expected Content-Type application/schema+json, got %q", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), ci.Schema) {
		t.Error("served body does not match the embedded schema")
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Errorf("served body is not valid JSON: %v", err)
	}
}
