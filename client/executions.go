package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type TriggerExecutionRequest struct {
	Workflow string `json:"workflow"`
	Ref      string `json:"ref,omitempty"`
}

func (c *Client) TriggerExecution(ctx context.Context, project, workflow, ref string) (Execution, error) {
	data, err := c.do(ctx, http.MethodPost, "/api/projects/"+project+"/executions", TriggerExecutionRequest{Workflow: workflow, Ref: ref})
	if err != nil {
		return Execution{}, err
	}
	var e Execution
	return e, json.Unmarshal(data, &e)
}

func (c *Client) RebuildExecution(ctx context.Context, id string) (Execution, error) {
	data, err := c.do(ctx, http.MethodPost, "/api/executions/"+id+"/rebuild", nil)
	if err != nil {
		return Execution{}, err
	}
	var e Execution
	return e, json.Unmarshal(data, &e)
}

func (c *Client) GetExecution(ctx context.Context, id string) (Execution, error) {
	data, err := c.get(ctx, "/api/executions/"+id)
	if err != nil {
		return Execution{}, err
	}
	var e Execution
	return e, json.Unmarshal(data, &e)
}

func (c *Client) ListExecutions(ctx context.Context, project string, limit int) ([]Execution, error) {
	data, err := c.get(ctx, fmt.Sprintf("/api/projects/%s/executions?limit=%d", project, limit))
	if err != nil {
		return nil, err
	}
	var executions []Execution
	return executions, json.Unmarshal(data, &executions)
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
	data, err := c.get(ctx, "/api/executions/"+id+"/artifacts")
	if err != nil {
		return nil, err
	}
	var artifacts []Artifact
	return artifacts, json.Unmarshal(data, &artifacts)
}

func (c *Client) DownloadArtifact(ctx context.Context, id, step, path string) ([]byte, error) {
	return c.get(ctx, "/api/executions/"+id+"/artifacts/"+step+"/"+path)
}
