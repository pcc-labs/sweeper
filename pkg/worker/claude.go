package worker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// anthropicEnvVars are stripped from every spawned sub-agent's environment so
// agents never authenticate with an inherited API token. When launched via
// `paperctl start`, paper's gateway owns authentication; when paper is
// absent, claude falls back to its own logged-in session. In tapes capture
// mode the base URL is re-pointed at the tapes proxy after stripping.
var anthropicEnvVars = map[string]bool{
	"ANTHROPIC_API_KEY":    true,
	"ANTHROPIC_AUTH_TOKEN": true, // bearer-token twin of API_KEY, used in proxy/gateway setups
	"ANTHROPIC_BASE_URL":   true,
}

// childEnv returns the parent environment with the Anthropic auth/proxy
// variables removed.
func childEnv() []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent))
	for _, kv := range parent {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if anthropicEnvVars[name] {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// claudeCommand builds the command that runs a claude sub-agent. Capture
// mode decides the wrapping: "paper" (and "auto" when the paperctl CLI is
// installed) launches via `paperctl start claude` so paper's gateway manages
// authentication and captures the session; "tapes" and "none" invoke claude
// directly — tapes capture happens via the proxy env set in the executor.
func claudeCommand(ctx context.Context, cfg ClaudeConfig, prompt string) *exec.Cmd {
	args := []string{"--print", "--dangerously-skip-permissions"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	args = append(args, cfg.ExtraArgs...)
	args = append(args, prompt)
	switch cfg.Capture.Mode {
	case CaptureModeTapes, CaptureModeNone:
		return exec.CommandContext(ctx, "claude", args...)
	}
	// paper and auto: wrap when the CLI is present. Explicit paper mode with
	// no CLI is rejected up front in cmd/run.go; falling through to a bare
	// claude here is a defensive last resort.
	if paperPath, ok := PaperBinary(); ok {
		return exec.CommandContext(ctx, paperPath, append([]string{"start", "claude", "--"}, args...)...)
	}
	return exec.CommandContext(ctx, "claude", args...)
}

// ClaudeConfig holds settings for the claude executor.
type ClaudeConfig struct {
	Model     string        // e.g. "claude-haiku-4-5"; empty uses the CLI's default
	ExtraArgs []string      // additional CLI arguments passed before the prompt
	Capture   CaptureConfig // session-capture gateway (paper, tapes, none)
}

// NewClaudeExecutor returns an Executor that runs claude sub-agents through
// paper (when available) with the Anthropic auth env stripped, so authentication
// is handled by paper's gateway rather than an inherited API token.
func NewClaudeExecutor(cfg ClaudeConfig) Executor {
	return func(ctx context.Context, task Task) Result {
		start := time.Now()
		prompt := task.Prompt
		if prompt == "" {
			prompt = BuildPrompt(task)
		}
		cmd := claudeCommand(ctx, cfg, prompt)
		cmd.Dir = task.Dir
		env := childEnv()
		if cfg.Capture.Mode == CaptureModeTapes {
			// Auth stays stripped (claude uses its own login); only the
			// base URL is re-pointed so traffic records in tapes.
			env = append(env, "ANTHROPIC_BASE_URL="+cfg.Capture.proxyOrDefault())
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
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
