package ci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
)

// streamStatus is sent as the "state" event so clients can follow the
// execution and its steps without re-fetching the whole execution.
type streamStatus struct {
	Status string             `json:"status"`
	Steps  []streamStepStatus `json:"steps"`
}

type streamStepStatus struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
}

// StreamLogs follows an execution's log files and emits Server-Sent Events
// until the execution reaches a terminal status or the context is canceled.
// Log lines are emitted as events named after their source ("setup" or a step
// name); a "state" event is emitted whenever the execution or its steps change
// status, and a final "done" event carries the terminal status. flush is
// called after every batch so the caller can push bytes to the client.
func (r *Runner) StreamLogs(ctx context.Context, projectID uuid.UUID, execID int64, w io.Writer, flush func()) {
	type streamState struct {
		offset  int64
		partial []byte
	}
	states := map[string]*streamState{}

	emit := func(source string, line []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", source, line)
	}
	sweep := func() {
		for _, path := range r.LogPaths(projectID, execID) {
			state := states[path]
			if state == nil {
				state = &streamState{}
				states[path] = state
			}
			fi, err := os.Stat(path)
			if err != nil || fi.Size() <= state.offset {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			_, _ = f.Seek(state.offset, io.SeekStart)
			data, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil || len(data) == 0 {
				continue
			}
			state.offset += int64(len(data))
			state.partial = append(state.partial, data...)
			for {
				idx := bytes.IndexByte(state.partial, '\n')
				if idx < 0 {
					break
				}
				line := state.partial[:idx]
				state.partial = state.partial[idx+1:]
				emit(LogSource(path), line)
			}
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastState string
	emitState := func(execution stores.Execution) {
		state := streamStatus{Status: string(execution.Status), Steps: make([]streamStepStatus, 0, len(execution.Steps))}
		for _, s := range execution.Steps {
			state.Steps = append(state.Steps, streamStepStatus{Name: s.Name, Status: string(s.Status), ExitCode: s.ExitCode})
		}
		data, err := json.Marshal(state)
		if err != nil || string(data) == lastState {
			return
		}
		lastState = string(data)
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
	}

	for {
		sweep()

		execution, err := r.Store.GetExecution(ctx, projectID, execID)
		if err != nil {
			return
		}
		emitState(execution)
		switch execution.Status {
		case stores.StatusSuccess, stores.StatusFailed, stores.StatusCanceled:
			sweep()
			for path, state := range states {
				if len(state.partial) > 0 {
					emit(LogSource(path), state.partial)
				}
			}
			fmt.Fprintf(w, "event: done\ndata: %s\n\n", execution.Status)
			flush()
			return
		}

		flush()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func LogSource(path string) string {
	base := filepath.Base(path)
	if base == "setup.log" {
		return "setup"
	}
	name := strings.TrimSuffix(base, ".log")
	if idx := strings.IndexByte(name, '_'); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}
