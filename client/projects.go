package client

import (
	"context"
	"encoding/json"
	"net/http"
)

func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (Project, error) {
	data, err := c.do(ctx, http.MethodPost, "/api/projects", req)
	if err != nil {
		return Project{}, err
	}
	var p Project
	return p, json.Unmarshal(data, &p)
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	data, err := c.get(ctx, "/api/projects")
	if err != nil {
		return nil, err
	}
	var projects []Project
	return projects, json.Unmarshal(data, &projects)
}

func (c *Client) GetProject(ctx context.Context, id string) (Project, error) {
	data, err := c.get(ctx, "/api/projects/"+id)
	if err != nil {
		return Project{}, err
	}
	var p Project
	return p, json.Unmarshal(data, &p)
}

func (c *Client) DeleteProject(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/projects/"+id, nil)
	return err
}
