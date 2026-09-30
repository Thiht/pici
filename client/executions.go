package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type TriggerExecutionRequest struct {
	Workflow string `json:"workflow"`
	Ref      string `json:"ref,omitempty"`
}

func (c *Client) TriggerExecution(ctx context.Context, project, workflow, ref string) (Execution, error) {
	return doJSON[Execution](c, ctx, http.MethodPost, "/api/projects/"+project+"/executions", TriggerExecutionRequest{Workflow: workflow, Ref: ref})
}

func (c *Client) RebuildExecution(ctx context.Context, project string, id int64) (Execution, error) {
	return doJSON[Execution](c, ctx, http.MethodPost, fmt.Sprintf("/api/projects/%s/executions/%d/rebuild", project, id), nil)
}

func (c *Client) RetryExecution(ctx context.Context, project string, id int64) (Execution, error) {
	return doJSON[Execution](c, ctx, http.MethodPost, fmt.Sprintf("/api/projects/%s/executions/%d/retry", project, id), nil)
}

func (c *Client) GetExecution(ctx context.Context, project string, id int64) (Execution, error) {
	return getJSON[Execution](c, ctx, fmt.Sprintf("/api/projects/%s/executions/%d", project, id))
}

func (c *Client) ListExecutions(ctx context.Context, project string, limit int) ([]Execution, error) {
	return getJSON[[]Execution](c, ctx, fmt.Sprintf("/api/projects/%s/executions?limit=%d", project, limit))
}

func (c *Client) CancelExecution(ctx context.Context, project string, id int64) error {
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/projects/%s/executions/%d/cancel", project, id), nil)
	return err
}

func (c *Client) ExecutionLogs(ctx context.Context, project string, id int64) (string, error) {
	data, err := c.get(ctx, fmt.Sprintf("/api/projects/%s/executions/%d/logs", project, id))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Client) StepLogs(ctx context.Context, project string, id int64, step string) (string, error) {
	data, err := c.get(ctx, fmt.Sprintf("/api/projects/%s/executions/%d/steps/%s/logs", project, id, step))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Event is a single Server-Sent Event from ExecutionStream.
type Event struct {
	Name string
	Data string
}

// ExecutionStream opens the SSE log stream for an execution. Events are named
// after their log source ("setup" or a step name); a "state" event carries the
// execution and step statuses, and a "done" event ends the stream. The caller
// must Close the returned stream.
func (c *Client) ExecutionStream(ctx context.Context, project string, id int64) (*EventStream, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/api/projects/%s/executions/%d/logs/stream", project, id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return nil, &httpError{status: resp.Status, body: strings.TrimSpace(string(data))}
	}
	return &EventStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body)}, nil
}

// EventStream reads Server-Sent Events until the stream ends or is closed.
type EventStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

func (s *EventStream) Close() error {
	return s.body.Close()
}

// Next returns the next event, or io.EOF when the stream ends.
func (s *EventStream) Next() (Event, error) {
	var ev Event
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" {
			if ev.Name != "" || ev.Data != "" {
				return ev, nil
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			ev.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			value := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			if ev.Data != "" {
				ev.Data += "\n"
			}
			ev.Data += value
		}
	}
	if err := s.scanner.Err(); err != nil {
		return ev, err
	}
	return ev, io.EOF
}

func (c *Client) ListArtifacts(ctx context.Context, project string, id int64) ([]Artifact, error) {
	return getJSON[[]Artifact](c, ctx, fmt.Sprintf("/api/projects/%s/executions/%d/artifacts", project, id))
}

func (c *Client) DownloadArtifact(ctx context.Context, project string, id int64, step, path string) ([]byte, error) {
	return c.get(ctx, fmt.Sprintf("/api/projects/%s/executions/%d/artifacts/%s/%s", project, id, step, path))
}
