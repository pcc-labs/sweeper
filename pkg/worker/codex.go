package worker

import (
	"context"
	"os/exec"
	"time"
)

// CodexConfig holds settings for the codex executor.
type CodexConfig struct {
	Model     string   // e.g. "o4-mini"; empty uses the CLI's default
	ExtraArgs []string // additional CLI arguments passed before the prompt
}

// NewCodexExecutor returns an Executor that invokes the codex CLI.
// Codex uses --quiet for minimal output and --approval-mode full-auto
// so it applies fixes without interactive approval.
func NewCodexExecutor(cfg CodexConfig) Executor {
	return func(ctx context.Context, task Task) Result {
		start := time.Now()
		prompt := task.Prompt
		if prompt == "" {
			prompt = BuildPrompt(task)
		}
		args := []string{"--quiet", "--approval-mode", "full-auto"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, cfg.ExtraArgs...)
		args = append(args, prompt)
		cmd := exec.CommandContext(ctx, "codex", args...)
		cmd.Dir = task.Dir
		out, err := cmd.CombinedOutput()
		duration := time.Since(start)
		if err != nil {
			return Result{
				TaskID: task.ID, File: task.File, Success: false,
				Output: string(out), Error: err.Error(), Duration: duration,
				Provider: "codex", Model: cfg.Model,
			}
		}
		return Result{
			TaskID: task.ID, File: task.File, Success: true,
			Output: string(out), Duration: duration, IssuesFix: len(task.Issues),
			Provider: "codex", Model: cfg.Model,
		}
	}
}
