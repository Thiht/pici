package client

import (
	"context"
	"encoding/json"
	"net/http"
)

func (c *Client) SetProjectVariable(ctx context.Context, project, key, value string, secret bool) error {
	_, err := c.do(ctx, http.MethodPost, "/api/projects/"+project+"/variables", Variable{Key: key, Value: value, Secret: secret})
	return err
}

func (c *Client) SetGlobalVariable(ctx context.Context, key, value string, secret bool) error {
	_, err := c.do(ctx, http.MethodPost, "/api/variables", Variable{Key: key, Value: value, Secret: secret})
	return err
}

func (c *Client) ListProjectVariables(ctx context.Context, project string) ([]Variable, error) {
	data, err := c.get(ctx, "/api/projects/"+project+"/variables")
	if err != nil {
		return nil, err
	}
	var variables []Variable
	return variables, json.Unmarshal(data, &variables)
}

func (c *Client) ListGlobalVariables(ctx context.Context) ([]Variable, error) {
	data, err := c.get(ctx, "/api/variables")
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
