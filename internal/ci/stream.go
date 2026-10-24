package ci

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

// StreamEvent is one step of an Execution's log stream: a line of output, a
// change in status, or the terminal status.
type StreamEvent interface {
	isStreamEvent()
}

// LogEvent is one line of output, from the setup phase or a Step.
type LogEvent struct {
	Source string
	Line   []byte
}

// StateEvent carries an Execution as it stands now, so a live client can
// follow along without re-fetching it.
type StateEvent struct {
	State ExecutionState
}

// DoneEvent carries the terminal status and ends the stream.
type DoneEvent struct {
	Status stores.Status
}

func (LogEvent) isStreamEvent()   {}
func (StateEvent) isStreamEvent() {}
func (DoneEvent) isStreamEvent()  {}

// ExecutionState is an Execution's progress while it runs.
type ExecutionState struct {
	Status stores.Status `json:"status"`
	Steps  []StepState   `json:"steps"`
}

// StepState is one Step's progress within an ExecutionState.
type StepState struct {
	Name       string            `json:"name"`
	Status     stores.StepStatus `json:"status"`
	ExitCode   int               `json:"exit_code"`
	StartedAt  *time.Time        `json:"started_at,omitempty"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Timeout    time.Duration     `json:"timeout,omitempty"`
}

// StreamLogs follows an Execution's log files and reports what happens until
// it reaches a terminal status or ctx is canceled: each line as a LogEvent, a
// StateEvent whenever the Execution or a Step changes, and a DoneEvent at the
// end. It is up to the caller to encode those events: how a client receives
// them is a transport decision.
func (r *Runner) StreamLogs(ctx context.Context, projectID uuid.UUID, execID int64, emit func(StreamEvent) error) error {
	type streamState struct {
		offset  int64
		partial []byte
	}
	states := map[string]*streamState{}

	sweep := func() error {
		for _, path := range r.Workspace.LogPaths(projectID, execID) {
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
				if err := emit(LogEvent{Source: workspace.LogSource(path), Line: line}); err != nil {
					return err
				}
			}
		}
		return nil
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var last ExecutionState
	var emitted bool

	for {
		if err := sweep(); err != nil {
			return err
		}

		execution, err := r.Store.GetAttempt(ctx, projectID, execID)
		if err != nil {
			return err
		}
		state := executionState(execution)
		if !emitted || !reflect.DeepEqual(last, state) {
			last = state
			emitted = true
			if err := emit(StateEvent{State: state}); err != nil {
				return err
			}
		}

		switch execution.Status {
		case stores.StatusSuccess, stores.StatusFailed, stores.StatusCanceled:
			if err := sweep(); err != nil {
				return err
			}
			for path, state := range states {
				if len(state.partial) > 0 {
					if err := emit(LogEvent{Source: workspace.LogSource(path), Line: state.partial}); err != nil {
						return err
					}
				}
			}
			return emit(DoneEvent{Status: execution.Status})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func executionState(execution stores.Execution) ExecutionState {
	state := ExecutionState{Status: execution.Status, Steps: make([]StepState, 0, len(execution.Steps))}
	for _, step := range execution.Steps {
		state.Steps = append(state.Steps, StepState{
			Name:       step.Name,
			Status:     step.Status,
			ExitCode:   step.ExitCode,
			StartedAt:  step.StartedAt,
			FinishedAt: step.FinishedAt,
			Timeout:    step.Timeout,
		})
	}
	return state
}
