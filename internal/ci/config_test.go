package ci

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseValidConfig(t *testing.T) {
	data := []byte(`
name: build
steps:
  - name: install
    script: install.sh
    timeout: 5m
  - name: test
    script: test.py
    depends_on: [install]
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "build" {
		t.Errorf("expected name build, got %q", cfg.Name)
	}
	if len(cfg.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(cfg.Steps))
	}
	if time.Duration(cfg.Steps[0].Timeout).Minutes() != 5 {
		t.Errorf("expected 5m timeout, got %v", time.Duration(cfg.Steps[0].Timeout))
	}
}

func TestParseStepDocker(t *testing.T) {
	data := []byte(`
steps:
  - name: build
    run: go build ./...
  - name: image
    run: docker build -t app .
    docker: true
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Steps[0].Docker {
		t.Error("first step should not have docker enabled")
	}
	if !cfg.Steps[1].Docker {
		t.Error("second step should have docker enabled")
	}
}

func TestParseOnForms(t *testing.T) {
	steps := "steps:\n  - name: a\n    run: echo hi\n"

	t.Run("absent", func(t *testing.T) {
		cfg, err := Parse([]byte(steps))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On != nil {
			t.Fatalf("On = %+v, want nil", cfg.On)
		}
	})

	t.Run("scalar", func(t *testing.T) {
		cfg, err := Parse([]byte("on: push\n" + steps))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On == nil || cfg.On.Push == nil || cfg.On.PullRequest != nil {
			t.Fatalf("On = %+v, want push only", cfg.On)
		}
	})

	t.Run("list", func(t *testing.T) {
		cfg, err := Parse([]byte("on: [push, pull_request]\n" + steps))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On == nil || cfg.On.Push == nil || cfg.On.PullRequest == nil {
			t.Fatalf("On = %+v, want push and pull_request", cfg.On)
		}
	})

	t.Run("empty mapping", func(t *testing.T) {
		cfg, err := Parse([]byte("on: {}\n" + steps))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On == nil || cfg.On.Push != nil || cfg.On.PullRequest != nil || cfg.On.Schedule != "" {
			t.Fatalf("On = %+v, want empty", cfg.On)
		}
	})

	t.Run("mapping with filters", func(t *testing.T) {
		data := "on:\n  push:\n    branches: [main]\n    tags: [\"v*\"]\n  pull_request:\n    branches: [main]\n  schedule: \"0 0 * * 0\"\n" + steps
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On.Push == nil || len(cfg.On.Push.Branches) != 1 || len(cfg.On.Push.Tags) != 1 {
			t.Fatalf("push = %+v", cfg.On.Push)
		}
		if cfg.On.PullRequest == nil || len(cfg.On.PullRequest.Branches) != 1 {
			t.Fatalf("pull_request = %+v", cfg.On.PullRequest)
		}
		if cfg.On.Schedule != "0 0 * * 0" {
			t.Fatalf("schedule = %q", cfg.On.Schedule)
		}
	})

	t.Run("push empty value", func(t *testing.T) {
		cfg, err := Parse([]byte("on:\n  push:\n" + steps))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On.Push == nil {
			t.Fatalf("push = nil, want unrestricted")
		}
	})

	t.Run("manual filter", func(t *testing.T) {
		data := "on:\n  manual:\n    branches: [main]\n    tags: [\"v*\"]\n" + steps
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On.Manual == nil || len(cfg.On.Manual.Branches) != 1 || len(cfg.On.Manual.Tags) != 1 {
			t.Fatalf("manual = %+v", cfg.On.Manual)
		}
	})

	t.Run("manual empty value", func(t *testing.T) {
		cfg, err := Parse([]byte("on:\n  manual:\n" + steps))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.On.Manual == nil {
			t.Fatalf("manual = nil, want unrestricted")
		}
	})
}

func TestParseOnErrors(t *testing.T) {
	steps := "steps:\n  - name: a\n    run: echo hi\n"
	tests := []struct {
		name string
		on   string
	}{
		{"unknown event", "on: merge_request\n"},
		{"unknown push key", "on:\n  push:\n    foo: bar\n"},
		{"unknown pull_request key", "on:\n  pull_request:\n    foo: bar\n"},
		{"pull_request tags", "on:\n  pull_request:\n    tags: [\"v*\"]\n"},
		{"scalar manual", "on: manual\n"},
		{"list manual", "on: [push, manual]\n"},
		{"unknown manual key", "on:\n  manual:\n    foo: bar\n"},
		{"scalar schedule", "on: schedule\n"},
		{"null schedule", "on:\n  schedule:\n"},
		{"invalid on type", "on: 42\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.on + steps)); err == nil {
				t.Fatalf("expected error for %q", tt.on)
			}
		})
	}
}

func TestParseRemovedKeys(t *testing.T) {
	steps := "steps:\n  - name: a\n    run: echo hi\n"
	for _, key := range []string{"schedule", "tags", "branches", "triggers"} {
		t.Run(key, func(t *testing.T) {
			if _, err := Parse([]byte(key + ": x\n" + steps)); err == nil {
				t.Fatalf("expected error for removed key %q", key)
			}
		})
	}
}

func TestEffectiveSchedule(t *testing.T) {
	steps := "steps:\n  - name: a\n    run: echo hi\n"

	cfg, err := Parse([]byte("on:\n  schedule: \"0 0 * * 0\"\n" + steps))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveSchedule(); got != "0 0 * * 0" {
		t.Fatalf("EffectiveSchedule = %q, want %q", got, "0 0 * * 0")
	}

	cfg, err = Parse([]byte(steps))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveSchedule(); got != "" {
		t.Fatalf("EffectiveSchedule = %q, want empty", got)
	}
}

func TestParseNoSteps(t *testing.T) {
	if _, err := Parse([]byte("name: build\nsteps: []\n")); err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestParseStepMissingName(t *testing.T) {
	data := []byte("steps:\n  - script: foo.sh\n")
	if _, err := Parse(data); err == nil {
		t.Fatal("expected error for missing step name")
	}
}

func TestParseStepMissingCommand(t *testing.T) {
	data := []byte("steps:\n  - name: foo\n")
	if _, err := Parse(data); err == nil {
		t.Fatal("expected error for step without script or run")
	}
}

func TestOrderSteps(t *testing.T) {
	steps := []Step{
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"a"}},
		{Name: "d", DependsOn: []string{"b", "c"}},
	}
	order, err := orderSteps(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	for _, s := range steps {
		for _, d := range s.DependsOn {
			if pos[d] > pos[s.Name] {
				t.Errorf("step %q ran before its dependency %q", s.Name, d)
			}
		}
	}
}

func TestOrderStepsCycle(t *testing.T) {
	steps := []Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	if _, err := orderSteps(steps); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestOrderStepsUnknownDep(t *testing.T) {
	steps := []Step{
		{Name: "a", DependsOn: []string{"nope"}},
	}
	if _, err := orderSteps(steps); err == nil {
		t.Fatal("expected unknown dependency error")
	}
}

func TestInterpreterShebang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env python3\nprint('hi')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, err := interpreterCommand(path, "/workspace/.ci/build/run.sh")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/env", "python3", "/workspace/.ci/build/run.sh"}
	if len(cmd) != len(want) {
		t.Fatalf("got %v, want %v", cmd, want)
	}
	for i := range want {
		if cmd[i] != want[i] {
			t.Fatalf("got %v, want %v", cmd, want)
		}
	}
}

func TestInterpreterExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.py")
	if err := os.WriteFile(path, []byte("print('hi')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, err := interpreterCommand(path, "/workspace/test.py")
	if err != nil {
		t.Fatal(err)
	}
	if cmd[0] != "python3" {
		t.Fatalf("expected python3, got %v", cmd)
	}
}
