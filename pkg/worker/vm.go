package worker

import (
	"context"
	"time"
)

// VMExecer is the interface the VM executor needs.
type VMExecer interface {
	Exec(ctx context.Context, args ...string) ([]byte, error)
}

// VMExecConfig holds settings for the VM executor.
type VMExecConfig struct {
	Model string // e.g. "claude-haiku-4-5"; empty uses the CLI's default
}

// NewVMExecutor returns an Executor that runs claude inside a stereOS VM.
func NewVMExecutor(vm VMExecer, cfg VMExecConfig) Executor {
	return func(ctx context.Context, task Task) Result {
		start := time.Now()
		args := []string{"claude", "--print", "--dangerously-skip-permissions"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, task.Prompt)
		out, err := vm.Exec(ctx, args...)
		duration := time.Since(start)
		if err != nil {
			return Result{
				TaskID: task.ID, File: task.File, Success: false,
				Output: string(out), Error: err.Error(), Duration: duration,
				Provider: "claude", Model: cfg.Model,
			}
		}
		return Result{
			TaskID: task.ID, File: task.File, Success: true,
			Output: string(out), Duration: duration, IssuesFix: len(task.Issues),
			Provider: "claude", Model: cfg.Model,
		}
	}
}
