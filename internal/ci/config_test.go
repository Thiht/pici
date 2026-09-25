package ci

import (
	"os"
	"path/filepath"
	"testing"
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
	if cfg.Steps[0].Timeout.Std().Minutes() != 5 {
		t.Errorf("expected 5m timeout, got %v", cfg.Steps[0].Timeout.Std())
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
