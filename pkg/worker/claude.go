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
// `paper start`, paper's gateway owns authentication; when paper is absent,
// claude falls back to its own logged-in session.
var anthropicEnvVars = map[string]bool{
	"ANTHROPIC_API_KEY":  true,
	"ANTHROPIC_BASE_URL": true,
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

// claudeCommand builds the command that runs a claude sub-agent. When the paper
// CLI is available the agent is launched via `paper start claude` so paper's
// gateway manages authentication and captures the session; otherwise it falls
// back to invoking claude directly (using claude's own login, without capture).
func claudeCommand(ctx context.Context, prompt string) *exec.Cmd {
	args := []string{"--print", "--dangerously-skip-permissions", prompt}
	if paperPath, err := exec.LookPath("paper"); err == nil {
		return exec.CommandContext(ctx, paperPath, append([]string{"start", "claude", "--"}, args...)...)
	}
	return exec.CommandContext(ctx, "claude", args...)
}

// NewClaudeExecutor returns an Executor that runs claude sub-agents through
// paper (when available) with the Anthropic auth env stripped, so authentication
// is handled by paper's gateway rather than an inherited API token.
func NewClaudeExecutor() Executor {
	return func(ctx context.Context, task Task) Result {
		start := time.Now()
		prompt := task.Prompt
		if prompt == "" {
			prompt = BuildPrompt(task)
		}
		cmd := claudeCommand(ctx, prompt)
		cmd.Dir = task.Dir
		cmd.Env = childEnv()
		out, err := cmd.CombinedOutput()
		duration := time.Since(start)
		if err != nil {
			return Result{
				TaskID: task.ID, File: task.File, Success: false,
				Output: string(out), Error: err.Error(), Duration: duration,
				Provider: "claude",
			}
		}
		return Result{
			TaskID: task.ID, File: task.File, Success: true,
			Output: string(out), Duration: duration, IssuesFix: len(task.Issues),
			Provider: "claude",
		}
	}
}
