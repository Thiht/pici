package ci

import (
	"fmt"
	"time"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Name        string            `yaml:"name"`
	Image       string            `yaml:"image"`
	Env         map[string]string `yaml:"env"`
	On          *On               `yaml:"on"`
	Paths       []string          `yaml:"paths"`
	PathsIgnore []string          `yaml:"paths_ignore"`
	Cache       []string          `yaml:"cache"`
	Concurrency string            `yaml:"concurrency"`
	Steps       []Step            `yaml:"steps"`
}

// On describes which events run a workflow. A nil On (the key is absent) means
// the implicit default: push on the project's default branch and pull requests
// targeting it. A non-nil On is authoritative: only its declared events run.
type On struct {
	Push        *RefFilter         `json:"push,omitempty"`
	PullRequest *PullRequestConfig `json:"pull_request,omitempty"`
	Manual      *RefFilter         `json:"manual,omitempty"`
	Schedule    string             `json:"schedule,omitempty"`
}

// RefFilter restricts an event to matching branch and/or tag refs. An empty
// list is treated as "not set".
type RefFilter struct {
	Branches []string `json:"branches,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type PullRequestConfig struct {
	Branches []string `json:"branches,omitempty"`
}

type Step struct {
	Name      string            `yaml:"name"`
	Script    string            `yaml:"script"`
	Run       string            `yaml:"run"`
	Timeout   Duration          `yaml:"timeout"`
	DependsOn []string          `yaml:"depends_on"`
	Env       map[string]string `yaml:"env"`
	Retry     int               `yaml:"retry"`
	Artifacts []string          `yaml:"artifacts"`
	Docker    bool              `yaml:"docker"`
}

type Duration time.Duration

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	pd, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(pd)
	return nil
}

// UnmarshalYAML accepts the three shorthand forms (`on: push`,
// `on: [push, pull_request]`, and a mapping) plus an empty value, which means
// no automatic trigger (only manual runs).
func (o *On) UnmarshalYAML(data []byte) error {
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		return err
	}

	*o = On{}
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return o.setEvent(t, nil, true)
	case []any:
		for _, e := range t {
			name, ok := e.(string)
			if !ok {
				return fmt.Errorf("invalid `on` entry: expected an event name")
			}
			if err := o.setEvent(name, nil, true); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for name, val := range t {
			if err := o.setEvent(name, val, false); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid `on`: expected an event name, a list of events, or a mapping")
	}
}

func (o *On) setEvent(name string, val any, shorthand bool) error {
	switch name {
	case "push":
		cfg, err := parseRefFilter(val, "on.push")
		if err != nil {
			return err
		}
		o.Push = cfg
		return nil
	case "manual":
		if shorthand {
			return fmt.Errorf("`manual` only accepts a mapping (`on.manual: {…}`); use `on: {}` for manual-only")
		}
		cfg, err := parseRefFilter(val, "on.manual")
		if err != nil {
			return err
		}
		o.Manual = cfg
		return nil
	case "pull_request":
		cfg, err := parsePullRequest(val)
		if err != nil {
			return err
		}
		o.PullRequest = cfg
		return nil
	case "schedule":
		s, ok := val.(string)
		if !ok || s == "" {
			return fmt.Errorf("`on.schedule` must be a cron expression")
		}
		o.Schedule = s
		return nil
	default:
		return fmt.Errorf("unknown event %q in `on`", name)
	}
}

func parseRefFilter(val any, field string) (*RefFilter, error) {
	if val == nil {
		return &RefFilter{}, nil
	}
	m, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid `%s`: expected a mapping or an empty value", field)
	}
	cfg := &RefFilter{}
	for k, v := range m {
		switch k {
		case "branches":
			list, err := toStrings(v)
			if err != nil {
				return nil, fmt.Errorf("%s.branches: %w", field, err)
			}
			cfg.Branches = list
		case "tags":
			list, err := toStrings(v)
			if err != nil {
				return nil, fmt.Errorf("%s.tags: %w", field, err)
			}
			cfg.Tags = list
		default:
			return nil, fmt.Errorf("unknown key %q in `%s`", k, field)
		}
	}
	return cfg, nil
}

func parsePullRequest(val any) (*PullRequestConfig, error) {
	if val == nil {
		return &PullRequestConfig{}, nil
	}
	m, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid `on.pull_request`: expected a mapping or an empty value")
	}
	cfg := &PullRequestConfig{}
	for k, v := range m {
		switch k {
		case "branches":
			list, err := toStrings(v)
			if err != nil {
				return nil, fmt.Errorf("on.pull_request.branches: %w", err)
			}
			cfg.Branches = list
		case "tags":
			return nil, fmt.Errorf("`on.pull_request.tags` is not supported: pull requests have no tags")
		default:
			return nil, fmt.Errorf("unknown key %q in `on.pull_request`", k)
		}
	}
	return cfg, nil
}

func toStrings(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list of strings")
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("expected a list of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

// removedRootKeys are the flat keys replaced by `on:`. They fail with a
// migration hint instead of a generic "unknown field".
var removedRootKeys = []string{"schedule", "tags", "branches", "triggers"}

func Parse(data []byte) (Config, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err == nil {
		for _, k := range removedRootKeys {
			if _, ok := root[k]; ok {
				return Config{}, fmt.Errorf("`%s` is no longer supported at the top level; declare it under `on:`", k)
			}
		}
	}

	var cfg Config
	if err := yaml.UnmarshalWithOptions(data, &cfg, yaml.Strict()); err != nil {
		return Config{}, err
	}

	if len(cfg.Steps) == 0 {
		return Config{}, fmt.Errorf("workflow has no steps")
	}
	for i, s := range cfg.Steps {
		if s.Name == "" {
			return Config{}, fmt.Errorf("step %d is missing a name", i)
		}
		if s.Script == "" && s.Run == "" {
			return Config{}, fmt.Errorf("step %q must define either script or run", s.Name)
		}
		if s.Retry < 0 {
			return Config{}, fmt.Errorf("step %q has a negative retry", s.Name)
		}
	}
	return cfg, nil
}
