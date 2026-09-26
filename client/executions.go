package client

import (
	"context"
	"fmt"
	"net/http"
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

func (c *Client) ListArtifacts(ctx context.Context, project string, id int64) ([]Artifact, error) {
	return getJSON[[]Artifact](c, ctx, fmt.Sprintf("/api/projects/%s/executions/%d/artifacts", project, id))
}

func (c *Client) DownloadArtifact(ctx context.Context, project string, id int64, step, path string) ([]byte, error) {
	return c.get(ctx, fmt.Sprintf("/api/projects/%s/executions/%d/artifacts/%s/%s", project, id, step, path))
}
