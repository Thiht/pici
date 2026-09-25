package client

import (
	"context"
	"encoding/json"
	"net/http"
)

func (c *Client) SetProjectVariable(ctx context.Context, project, key, value string, secret bool) error {
	return c.setVariable(ctx, "/api/projects/"+project+"/variables", Variable{Key: key, Value: value, Secret: secret})
}

func (c *Client) SetGlobalVariable(ctx context.Context, key, value string, secret bool) error {
	return c.setVariable(ctx, "/api/variables", Variable{Key: key, Value: value, Secret: secret})
}

func (c *Client) setVariable(ctx context.Context, path string, v Variable) error {
	_, err := c.do(ctx, http.MethodPost, path, v)
	return err
}

func (c *Client) ListProjectVariables(ctx context.Context, project string) ([]Variable, error) {
	return c.listVariables(ctx, "/api/projects/"+project+"/variables")
}

func (c *Client) ListGlobalVariables(ctx context.Context) ([]Variable, error) {
	return c.listVariables(ctx, "/api/variables")
}

func (c *Client) listVariables(ctx context.Context, path string) ([]Variable, error) {
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var variables []Variable
	return variables, json.Unmarshal(data, &variables)
}

func (c *Client) DeleteProjectVariable(ctx context.Context, project, key string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/projects/"+project+"/variables/"+key, nil)
	return err
}

func (c *Client) DeleteGlobalVariable(ctx context.Context, key string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/variables/"+key, nil)
	return err
}
