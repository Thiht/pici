package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
)

func TestSchemaRoute(t *testing.T) {
	svc := auth.NewService(nil, &fakeProvider{}, nil, nil, time.Hour, false)
	handler := New(nil, nil, "", svc, "test").Routes()

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
