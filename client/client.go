package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	Provider      string `json:"provider"`
	AuthType      string `json:"auth_type"`
	AuthUser      string `json:"auth_user"`
	DefaultBranch string `json:"default_branch"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type CreateProjectRequest struct {
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	Provider      string `json:"provider,omitempty"`
	AuthType      string `json:"auth_type,omitempty"`
	AuthUser      string `json:"auth_user,omitempty"`
	AuthSecret    string `json:"auth_secret,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type Variable struct {
	ProjectID string `json:"project_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Secret    bool   `json:"secret"`
}

type StepResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	ExitCode   int    `json:"exit_code"`
	Error      string `json:"error,omitempty"`
}

type Execution struct {
	ID         string       `json:"id"`
	ProjectID  string       `json:"project_id"`
	Project    string       `json:"project,omitempty"`
	Workflow   string       `json:"workflow"`
	Ref        string       `json:"ref"`
	CommitSHA  string       `json:"commit_sha"`
	Status     string       `json:"status"`
	Trigger    string       `json:"trigger"`
	Steps      []StepResult `json:"steps,omitempty"`
	Error      string       `json:"error,omitempty"`
	StartedAt  int64        `json:"started_at"`
	FinishedAt int64        `json:"finished_at"`
	CreatedAt  int64        `json:"created_at"`
}

type Artifact struct {
	Step string `json:"step"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}
