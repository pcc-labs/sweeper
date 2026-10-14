package worker

import (
	"context"
	"time"
)

// VMExecer is the interface the VM executor needs.
type VMExecer interface {
	Exec(ctx context.Context, args ...string) ([]byte, error)
}

// VMSyncer is implemented by backends whose workspace is a remote copy of
// the target dir rather than a mount. After a task runs, the executor asks
// them to bring that file's changes back to the host.
type VMSyncer interface {
	SyncBack(ctx context.Context, hostPath string) error
}

// VMExecConfig holds settings for the VM executor.
type VMExecConfig struct {
	Model string // e.g. "claude-haiku-4-5"; empty uses the CLI's default
}

// NewVMExecutor returns an Executor that runs claude inside a sub-agent VM
// on any backend (stereOS, smolvm or Daytona). A backend that implements
// VMSyncer has the task's file pulled back after a successful run; a sync
// failure marks the task failed so a change never silently stays behind.
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
		if s, ok := vm.(VMSyncer); ok {
			if serr := s.SyncBack(ctx, task.File); serr != nil {
				return Result{
					TaskID: task.ID, File: task.File, Success: false,
					Output: string(out), Error: "sync back to host: " + serr.Error(), Duration: time.Since(start),
					Provider: "claude", Model: cfg.Model,
				}
			}
		}
		return Result{
			TaskID: task.ID, File: task.File, Success: true,
			Output: string(out), Duration: duration, IssuesFix: len(task.Issues),
			Provider: "claude", Model: cfg.Model,
		}
	}
}
