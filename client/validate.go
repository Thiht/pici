package client

import (
	"context"
	"net/http"
	"strings"
)

func (c *Client) Validate(ctx context.Context, yaml string) (string, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/validate", strings.NewReader(yaml))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/yaml")
	data, err := c.send(req)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
