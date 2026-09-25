package client

import (
	"context"
	"io"
	"net/http"
	"strings"
)

func (c *Client) Validate(ctx context.Context, yaml string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/validate", strings.NewReader(yaml))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/yaml")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", &httpError{status: resp.Status, body: strings.TrimSpace(string(data))}
	}
	return string(data), nil
}

type httpError struct {
	status string
	body   string
}

func (e *httpError) Error() string {
	return e.status + ": " + e.body
}
