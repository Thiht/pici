package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (r *Runner) LogDir(execID string) string {
	return filepath.Join(r.LogsDir, execID)
}

func (r *Runner) SetupLogPath(execID string) string {
	return filepath.Join(r.LogDir(execID), "setup.log")
}

func (r *Runner) StepLogPath(execID string, index int, name string) string {
	return filepath.Join(r.LogDir(execID), "steps", fmt.Sprintf("%03d_%s.log", index, sanitizeLogName(name)))
}

func (r *Runner) LogPaths(execID string) []string {
	var out []string
	setup := r.SetupLogPath(execID)
	if _, err := os.Stat(setup); err == nil {
		out = append(out, setup)
	}
	stepsDir := filepath.Join(r.LogDir(execID), "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		return out
	}
	var stepFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			stepFiles = append(stepFiles, filepath.Join(stepsDir, e.Name()))
		}
	}
	sort.Strings(stepFiles)
	return append(out, stepFiles...)
}

func sanitizeLogName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
