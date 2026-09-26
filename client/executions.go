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

func (c *Client) RebuildExecution(ctx context.Context, id string) (Execution, error) {
	return doJSON[Execution](c, ctx, http.MethodPost, "/api/executions/"+id+"/rebuild", nil)
}

func (c *Client) GetExecution(ctx context.Context, id string) (Execution, error) {
	return getJSON[Execution](c, ctx, "/api/executions/"+id)
}

func (c *Client) ListExecutions(ctx context.Context, project string, limit int) ([]Execution, error) {
	return getJSON[[]Execution](c, ctx, fmt.Sprintf("/api/projects/%s/executions?limit=%d", project, limit))
}

func (c *Client) CancelExecution(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/executions/"+id+"/cancel", nil)
	return err
}

func (c *Client) ExecutionLogs(ctx context.Context, id string) (string, error) {
	data, err := c.get(ctx, "/api/executions/"+id+"/logs")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Client) ListArtifacts(ctx context.Context, id string) ([]Artifact, error) {
	return getJSON[[]Artifact](c, ctx, "/api/executions/"+id+"/artifacts")
}

func (c *Client) DownloadArtifact(ctx context.Context, id, step, path string) ([]byte, error) {
	return c.get(ctx, "/api/executions/"+id+"/artifacts/"+step+"/"+path)
}
