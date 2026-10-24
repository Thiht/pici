package sse

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Thiht/pici/internal/ci"
)

// Writer writes execution stream events as Server-Sent Events, flushing after
// each one so a client sees it immediately.
type Writer struct {
	w     io.Writer
	flush func()
}

// New returns a Writer that writes to w and calls flush after each event.
// flush may be nil when the caller does not buffer.
func New(w io.Writer, flush func()) *Writer {
	return &Writer{w: w, flush: flush}
}

// Send writes one event. Log lines are named after their source, so a client
// can route them to the right Step; a state event carries the Execution as
// JSON; a done event carries the terminal status.
func (w *Writer) Send(event ci.StreamEvent) error {
	switch e := event.(type) {
	case ci.LogEvent:
		if _, err := fmt.Fprintf(w.w, "event: %s\ndata: %s\n\n", e.Source, e.Line); err != nil {
			return err
		}
	case ci.StateEvent:
		data, err := json.Marshal(e.State)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w.w, "event: state\ndata: %s\n\n", data); err != nil {
			return err
		}
	case ci.DoneEvent:
		if _, err := fmt.Fprintf(w.w, "event: done\ndata: %s\n\n", e.Status); err != nil {
			return err
		}
	default:
		return fmt.Errorf("sse: unsupported event %T", event)
	}
	if w.flush != nil {
		w.flush()
	}
	return nil
}
