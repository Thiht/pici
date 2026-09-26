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
	Schedule    string            `yaml:"schedule"`
	Tags        []string          `yaml:"tags"`
	Branches    []string          `yaml:"branches"`
	Paths       []string          `yaml:"paths"`
	PathsIgnore []string          `yaml:"paths_ignore"`
	Cache       []string          `yaml:"cache"`
	Concurrency string            `yaml:"concurrency"`
	Steps       []Step            `yaml:"steps"`
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
}

type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

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

func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
