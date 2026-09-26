package client

import (
	"context"
	"encoding/json"
	"net/http"
)

func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (Project, error) {
	return doJSON[Project](c, ctx, http.MethodPost, "/api/projects", req)
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	return getJSON[[]Project](c, ctx, "/api/projects")
}

func (c *Client) GetProject(ctx context.Context, id string) (Project, error) {
	return getJSON[Project](c, ctx, "/api/projects/"+id)
}

func (c *Client) ListWorkflows(ctx context.Context, project string) ([]string, error) {
	var resp struct {
		Workflows []string `json:"workflows"`
	}
	data, err := c.get(ctx, "/api/projects/"+project+"/configs")
	if err != nil {
		return nil, err
	}
	return resp.Workflows, json.Unmarshal(data, &resp)
}

func (c *Client) UpdateProject(ctx context.Context, id string, req UpdateProjectRequest) (Project, error) {
	return doJSON[Project](c, ctx, http.MethodPut, "/api/projects/"+id, req)
}

func (c *Client) DeleteProject(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/projects/"+id, nil)
	return err
}
