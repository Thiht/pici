package ci

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/mask"
	"github.com/Thiht/pici/internal/stores"
)

func (r *Runner) executeSteps(ctx context.Context, cfg Config, baseEnv []string, image, repoDir, wfDir, execID string, secrets, cacheBinds []string) ([]stores.StepResult, bool, bool) {
	steps := cfg.Steps
	order, err := orderSteps(steps)
	if err != nil {
		res := stores.StepResult{Name: "workflow", Status: stores.StepStatusFailed, Error: err.Error()}
		return []stores.StepResult{res}, true, false
	}

	pos := make(map[string]int, len(order))
	for i, n := range order {
		pos[n] = i
	}

	depsByName := make(map[string][]string, len(steps))
	children := make(map[string][]string, len(steps))
	for _, s := range steps {
		depsByName[s.Name] = s.DependsOn
		for _, d := range s.DependsOn {
			children[d] = append(children[d], s.Name)
		}
	}

	results := make([]stores.StepResult, len(steps))
	statuses := make([]string, len(steps))

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallelSteps)

	failed, canceled := false, false

	var launch func(name string)
	launch = func(name string) {
		step := findStep(steps, name)
		i := pos[name]
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			skip := false
			for _, d := range depsByName[name] {
				if statuses[pos[d]] != stores.StepStatusSuccess {
					skip = true
					break
				}
			}
			mu.Unlock()

			var res stores.StepResult
			switch {
			case skip:
				res = stores.StepResult{Name: name, Status: stores.StepStatusSkipped}
			case ctx.Err() != nil:
				res = stores.StepResult{Name: name, Status: stores.StepStatusCanceled, Error: "canceled"}
			default:
				res = r.runStep(ctx, step, baseEnv, image, repoDir, wfDir, execID, i, secrets, cacheBinds)
			}

			mu.Lock()
			results[i] = res
			statuses[i] = res.Status
			if res.Status == stores.StepStatusFailed {
				failed = true
			}
			if res.Status == stores.StepStatusCanceled {
				canceled = true
			}
			var ready []string
			for _, child := range children[name] {
				allDone := true
				for _, d := range depsByName[child] {
					if statuses[pos[d]] == "" {
						allDone = false
						break
					}
				}
				if allDone {
					ready = append(ready, child)
				}
			}
			mu.Unlock()

			for _, c := range ready {
				launch(c)
			}
		})
	}

	for _, s := range steps {
		if len(s.DependsOn) == 0 {
			launch(s.Name)
		}
	}
	wg.Wait()

	out := make([]stores.StepResult, len(steps))
	for i, n := range order {
		out[i] = results[pos[n]]
	}
	return out, failed, canceled
}

func (r *Runner) runStep(ctx context.Context, step Step, baseEnv []string, image, repoDir, wfDir, execID string, index int, secrets, cacheBinds []string) stores.StepResult {
	logPath := r.StepLogPath(execID, index, step.Name)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return stores.StepResult{Name: step.Name, Status: stores.StepStatusFailed, Error: err.Error(), FinishedAt: time.Now().UnixMilli()}
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return stores.StepResult{Name: step.Name, Status: stores.StepStatusFailed, Error: err.Error(), FinishedAt: time.Now().UnixMilli()}
	}
	mw := mask.NewWriter(file, secrets)
	defer func() {
		_ = mw.Flush()
		file.Close()
	}()

	attempts := step.Retry + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			fmt.Fprintf(mw, "\nretrying (%d/%d)...\n", attempt, attempts)
		}
		res := r.runStepOnce(ctx, step, baseEnv, image, repoDir, wfDir, mw, cacheBinds)
		if res.Status == stores.StepStatusSuccess || attempt == attempts || ctx.Err() != nil {
			if res.Status == stores.StepStatusSuccess {
				r.collectArtifacts(execID, index, step.Artifacts, repoDir)
			}
			return res
		}
	}
	return stores.StepResult{Name: step.Name, Status: stores.StepStatusFailed, Error: "unreachable", FinishedAt: time.Now().UnixMilli()}
}

func (r *Runner) runStepOnce(ctx context.Context, step Step, baseEnv []string, image, repoDir, wfDir string, logW *mask.Writer, cacheBinds []string) stores.StepResult {
	res := stores.StepResult{Name: step.Name, Status: stores.StepStatusRunning, StartedAt: time.Now().UnixMilli()}
	fmt.Fprintf(logW, "==> %s\n", step.Name)

	cmd, err := r.stepCommand(wfDir, step)
	if err != nil {
		fmt.Fprintf(logW, "command error: %v\n", err)
		res.Status = stores.StepStatusFailed
		res.Error = err.Error()
		res.FinishedAt = time.Now().UnixMilli()
		return res
	}

	stepEnv := append([]string{}, baseEnv...)
	for k, v := range step.Env {
		stepEnv = append(stepEnv, k+"="+v)
	}

	timeout := step.Timeout.Std()
	if timeout <= 0 {
		timeout = r.DefaultStepTimeout
	}

	binds := append([]string{repoDir + ":" + r.MountPath}, cacheBinds...)

	code, err := r.Engine.Run(ctx, docker.RunOptions{
		Image:      image,
		Command:    cmd,
		Env:        stepEnv,
		Binds:      binds,
		WorkingDir: r.MountPath,
		Logs:       logW,
		Timeout:    timeout,
	})
	res.FinishedAt = time.Now().UnixMilli()
	res.ExitCode = code

	switch {
	case ctx.Err() != nil:
		res.Status = stores.StepStatusCanceled
		res.Error = "canceled"
	case err != nil:
		res.Status = stores.StepStatusFailed
		res.Error = err.Error()
	case code != 0:
		res.Status = stores.StepStatusFailed
		res.Error = fmt.Sprintf("exit code %d", code)
	default:
		res.Status = stores.StepStatusSuccess
	}
	fmt.Fprintf(logW, "==> %s: %s\n", step.Name, res.Status)
	return res
}

func (r *Runner) stepCommand(wfDir string, step Step) ([]string, error) {
	if step.Run != "" {
		return []string{"sh", "-c", step.Run}, nil
	}
	hostPath := filepath.Join(wfDir, step.Script)
	workflow := filepath.Base(wfDir)
	containerPath := filepath.ToSlash(filepath.Join(r.MountPath, ".ci", workflow, step.Script))
	return interpreterCommand(hostPath, containerPath)
}

func interpreterCommand(hostPath, containerPath string) ([]string, error) {
	f, err := os.Open(hostPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	line, _ := bufio.NewReader(f).ReadString('\n')
	line = strings.TrimSpace(line)
	if after, ok := strings.CutPrefix(line, "#!"); ok {
		parts := strings.Fields(after)
		if len(parts) > 0 {
			return append(parts, containerPath), nil
		}
	}

	switch strings.ToLower(filepath.Ext(hostPath)) {
	case ".sh":
		return []string{"/bin/sh", containerPath}, nil
	case ".bash":
		return []string{"/bin/bash", containerPath}, nil
	case ".py":
		return []string{"python3", containerPath}, nil
	case ".js":
		return []string{"node", containerPath}, nil
	case ".rb":
		return []string{"ruby", containerPath}, nil
	case ".go":
		return []string{"go", "run", containerPath}, nil
	default:
		return []string{"/bin/sh", containerPath}, nil
	}
}

func orderSteps(steps []Step) ([]string, error) {
	names := make(map[string]bool, len(steps))
	for _, s := range steps {
		names[s.Name] = true
	}

	inDegree := make(map[string]int, len(steps))
	adj := make(map[string][]string, len(steps))
	for _, s := range steps {
		inDegree[s.Name] = 0
		for _, d := range s.DependsOn {
			if !names[d] {
				return nil, fmt.Errorf("step %q depends on unknown step %q", s.Name, d)
			}
			adj[d] = append(adj[d], s.Name)
			inDegree[s.Name]++
		}
	}

	var queue []string
	for _, s := range steps {
		if inDegree[s.Name] == 0 {
			queue = append(queue, s.Name)
		}
	}

	var order []string
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		for _, m := range adj[n] {
			inDegree[m]--
			if inDegree[m] == 0 {
				queue = append(queue, m)
			}
		}
	}

	if len(order) != len(steps) {
		return nil, fmt.Errorf("cycle detected in step dependencies")
	}
	return order, nil
}

func findStep(steps []Step, name string) Step {
	for _, s := range steps {
		if s.Name == name {
			return s
		}
	}
	return Step{}
}
