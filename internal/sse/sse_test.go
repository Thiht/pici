package sse

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func TestSend(t *testing.T) {
	var buf bytes.Buffer
	flushes := 0
	writer := New(&buf, func() { flushes++ })

	events := []ci.StreamEvent{
		ci.LogEvent{Source: "compile", Line: []byte("building")},
		ci.StateEvent{State: ci.ExecutionState{
			Status: stores.StatusSuccess,
			Steps:  []ci.StepState{{Name: "compile", Status: stores.StepStatusSuccess}},
		}},
		ci.DoneEvent{Status: stores.StatusSuccess},
	}
	for _, event := range events {
		if err := writer.Send(event); err != nil {
			t.Fatal(err)
		}
	}

	want := "event: compile\ndata: building\n\n" +
		`event: state` + "\n" + `data: {"status":"success","steps":[{"name":"compile","status":"success","exit_code":0}]}` + "\n\n" +
		"event: done\ndata: success\n\n"
	if got := buf.String(); got != want {
		t.Fatalf("stream =\n%q\nwant\n%q", got, want)
	}
	if flushes != len(events) {
		t.Fatalf("flushed %d times, want %d", flushes, len(events))
	}
}

func TestSendRejectsUnknownEvent(t *testing.T) {
	writer := New(&bytes.Buffer{}, func() {})
	if err := writer.Send(nil); err == nil || !strings.Contains(err.Error(), "unsupported event") {
		t.Fatalf("expected an unsupported event error, got %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestSendReturnsWriteError(t *testing.T) {
	flushed := false
	writer := New(failingWriter{}, func() { flushed = true })
	if err := writer.Send(ci.DoneEvent{Status: stores.StatusSuccess}); err == nil {
		t.Fatal("expected the write error")
	}
	if flushed {
		t.Fatal("a failed write should not flush")
	}
}
