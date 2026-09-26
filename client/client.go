package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/version"
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

	req, err := c.newRequest(ctx, method, path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) send(req *http.Request) ([]byte, error) {
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
		return nil, &httpError{status: resp.Status, body: strings.TrimSpace(string(data))}
	}
	return data, nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

func getJSON[T any](c *Client, ctx context.Context, path string) (T, error) {
	var out T
	data, err := c.get(ctx, path)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(data, &out)
}

func doJSON[T any](c *Client, ctx context.Context, method, path string, body any) (T, error) {
	var out T
	data, err := c.do(ctx, method, path, body)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(data, &out)
}

type httpError struct {
	status string
	body   string
}

func (e *httpError) Error() string {
	return e.status + ": " + e.body
}

type Health struct {
	Status string `json:"status"`
	Docker string `json:"docker,omitempty"`
	Queue  *int   `json:"queue,omitempty"`
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	return getJSON[Health](c, ctx, "/health")
}

type Version = version.Info

func (c *Client) Version(ctx context.Context) (Version, error) {
	return getJSON[Version](c, ctx, "/version")
}

type Project struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	RepoURL       string    `json:"repo_url"`
	Provider      Provider  `json:"provider"`
	AuthType      AuthType  `json:"auth_type"`
	AuthUser      string    `json:"auth_user"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateProjectRequest struct {
	Name          string   `json:"name"`
	RepoURL       string   `json:"repo_url"`
	Provider      Provider `json:"provider,omitempty"`
	AuthType      AuthType `json:"auth_type,omitempty"`
	AuthUser      string   `json:"auth_user,omitempty"`
	AuthSecret    string   `json:"auth_secret,omitempty"`
	WebhookSecret string   `json:"webhook_secret,omitempty"`
	DefaultBranch string   `json:"default_branch,omitempty"`
}

type UpdateProjectRequest struct {
	Name          string   `json:"name,omitempty"`
	RepoURL       string   `json:"repo_url,omitempty"`
	Provider      Provider `json:"provider,omitempty"`
	AuthType      AuthType `json:"auth_type,omitempty"`
	AuthUser      string   `json:"auth_user,omitempty"`
	AuthSecret    string   `json:"auth_secret,omitempty"`
	WebhookSecret string   `json:"webhook_secret,omitempty"`
	DefaultBranch string   `json:"default_branch,omitempty"`
}

type Variable struct {
	ProjectID *uuid.UUID `json:"project_id"`
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	Secret    bool       `json:"secret"`
}

type StepResult struct {
	Name       string     `json:"name"`
	Status     StepStatus `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   int        `json:"exit_code"`
	Error      string     `json:"error,omitempty"`
}

type Execution struct {
	ID              uuid.UUID    `json:"id"`
	ProjectID       uuid.UUID    `json:"project_id"`
	Project         string       `json:"project,omitempty"`
	Workflow        string       `json:"workflow"`
	Ref             string       `json:"ref"`
	CommitSHA       string       `json:"commit_sha"`
	Status          Status       `json:"status"`
	Trigger         Trigger      `json:"trigger"`
	Steps           []StepResult `json:"steps,omitempty"`
	Error           string       `json:"error,omitempty"`
	StartedAt       *time.Time   `json:"started_at,omitempty"`
	SetupFinishedAt *time.Time   `json:"setup_finished_at,omitempty"`
	FinishedAt      *time.Time   `json:"finished_at,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
}

type Artifact struct {
	Step string `json:"step"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}
