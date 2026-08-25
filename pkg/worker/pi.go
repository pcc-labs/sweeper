package worker

import (
	"context"
	"os/exec"
	"time"
)

// PiConfig holds settings for the pi executor.
type PiConfig struct {
	Model     string        // a model registered with pi (~/.pi/agent/models.json); empty uses pi's default
	ExtraArgs []string      // additional CLI arguments passed before the prompt (e.g. -e extension.ts)
	Capture   CaptureConfig // session-capture gateway (paper, tapes, none)
}

// NewPiExecutor returns an Executor that invokes the pi coding agent CLI in
// print mode (-p). Model resolution — providers, endpoints, credentials — is
// owned by pi's own registry in ~/.pi/agent/models.json, so any model
// registered there (cloud providers, local Ollama, a semantic router) is
// reachable by name without sweeper-side endpoint config. pi reads provider
// credentials from the environment, so unlike the claude executor the parent
// environment is passed through unchanged.
func NewPiExecutor(cfg PiConfig) Executor {
	return func(ctx context.Context, task Task) Result {
		start := time.Now()
		prompt := task.Prompt
		if prompt == "" {
			prompt = BuildPrompt(task)
		}
		args := []string{"-p"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, cfg.ExtraArgs...)
		args = append(args, prompt)
		var cmd *exec.Cmd
		switch cfg.Capture.Mode {
		case CaptureModePaper:
			if paperPath, ok := PaperBinary(); ok {
				// paperctl start supports pi; the gateway owns auth, so the
				// inherited Anthropic env is stripped like the claude path.
				cmd = exec.CommandContext(ctx, paperPath, append([]string{"start", "pi", "--"}, args...)...)
				cmd.Env = childEnv()
			} else {
				cmd = exec.CommandContext(ctx, "pi", args...)
			}
		case CaptureModeTapes:
			cmd = exec.CommandContext(ctx, "pi", args...)
			cmd.Env = envWith("ANTHROPIC_BASE_URL", cfg.Capture.proxyOrDefault())
		default:
			// auto/none: pi owns its providers and credentials; pass the
			// environment through unchanged.
			cmd = exec.CommandContext(ctx, "pi", args...)
		}
		cmd.Dir = task.Dir
		out, err := cmd.CombinedOutput()
		duration := time.Since(start)
		if err != nil {
			return Result{
				TaskID: task.ID, File: task.File, Success: false,
				Output: string(out), Error: err.Error(), Duration: duration,
				Provider: "pi", Model: cfg.Model,
			}
		}
		return Result{
			TaskID: task.ID, File: task.File, Success: true,
			Output: string(out), Duration: duration, IssuesFix: len(task.Issues),
			Provider: "pi", Model: cfg.Model,
		}
	}
}
