package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/linter"
)

func TestBuildPrompt(t *testing.T) {
	task := Task{
		File: "server.go",
		Issues: []linter.Issue{
			{File: "server.go", Line: 42, Message: "err is not used", Linter: "ineffassign"},
			{File: "server.go", Line: 55, Message: "exported function should have comment", Linter: "revive"},
		},
	}
	prompt := BuildPrompt(task)
	if !strings.Contains(prompt, "server.go") {
		t.Error("prompt should reference the file")
	}
	if !strings.Contains(prompt, "Line 42") {
		t.Error("prompt should include line numbers")
	}
	if !strings.Contains(prompt, "ineffassign") {
		t.Error("prompt should include linter names")
	}
}

func TestBuildRawPrompt(t *testing.T) {
	task := Task{
		Dir:       "/tmp/project",
		RawOutput: "ERROR: something went wrong\n  --> src/lib.rs:45\n",
	}
	prompt := BuildRawPrompt(task)
	if !strings.Contains(prompt, "ERROR: something went wrong") {
		t.Error("raw prompt should contain the original output")
	}
	if !strings.Contains(prompt, "Analyze it") {
		t.Error("raw prompt should instruct the agent to analyze")
	}
	if !strings.Contains(prompt, "Commit nothing") {
		t.Error("raw prompt should instruct not to commit")
	}
}

func TestClaudeExecutorUsesTaskPrompt(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	// Script echoes the prompt argument (last arg) so we can verify it
	// Args: --print --dangerously-skip-permissions <prompt>
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	customPrompt := "custom retry prompt for testing"
	task := Task{
		ID:   0,
		File: "test.go",
		Dir:  t.TempDir(),
		Issues: []linter.Issue{
			{File: "test.go", Line: 1, Message: "unused var", Linter: "revive"},
		},
		Prompt: customPrompt,
	}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, customPrompt) {
		t.Errorf("expected executor to use task.Prompt %q, got output: %s", customPrompt, result.Output)
	}
}

func TestClaudeExecutorFallsBackToBuildPrompt(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	task := Task{
		ID:   0,
		File: "test.go",
		Dir:  t.TempDir(),
		Issues: []linter.Issue{
			{File: "test.go", Line: 1, Message: "unused var", Linter: "revive"},
		},
		// Prompt intentionally left empty
	}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	// When Prompt is empty, BuildPrompt output should be used
	if !strings.Contains(result.Output, "test.go") {
		t.Errorf("expected BuildPrompt output referencing file, got: %s", result.Output)
	}
}

func TestClaudeExecutorLaunchesViaPaper(t *testing.T) {
	dir := t.TempDir()
	// Fake paper echoes its args so we can confirm `paper start claude -- ...`.
	fakePaper := filepath.Join(dir, "paper")
	if err := os.WriteFile(fakePaper, []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	prompt := "fix the thing"
	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: prompt}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "start claude --") {
		t.Errorf("expected launch via `paper start claude --`, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, prompt) {
		t.Errorf("expected prompt passed through to claude, got: %s", result.Output)
	}
}

func TestChildEnvStripsAnthropicVars(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:5000")
	t.Setenv("PATH", "/usr/bin")
	env := childEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") || strings.HasPrefix(kv, "ANTHROPIC_BASE_URL=") {
			t.Errorf("expected Anthropic auth var stripped, found: %s", kv)
		}
	}
	// Unrelated vars (e.g. PATH) must be preserved so the agent still runs.
	foundPath := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			foundPath = true
		}
	}
	if !foundPath {
		t.Error("expected PATH to be preserved in child env")
	}
}

func TestClaudeExecutorSuccess(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho fixed"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	task := Task{
		ID:   0,
		File: "test.go",
		Dir:  t.TempDir(),
		Issues: []linter.Issue{
			{File: "test.go", Line: 1, Message: "unused var", Linter: "revive"},
		},
	}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.IssuesFix != 1 {
		t.Errorf("expected 1 issue fix, got %d", result.IssuesFix)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestClaudeExecutorError(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho error output; exit 1"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	task := Task{
		ID:   0,
		File: "test.go",
		Dir:  t.TempDir(),
		Issues: []linter.Issue{
			{File: "test.go", Line: 1, Message: "unused var", Linter: "revive"},
		},
	}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if result.Success {
		t.Error("expected failure")
	}
	if result.Output == "" {
		t.Error("expected output even on error")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestClaudeExecutorPassesModelFlag(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewClaudeExecutor(ClaudeConfig{Model: "claude-haiku-4-5"})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "--model claude-haiku-4-5") {
		t.Errorf("expected --model flag passed to claude, got: %s", result.Output)
	}
	if result.Model != "claude-haiku-4-5" {
		t.Errorf("expected result.Model recorded, got %q", result.Model)
	}
}

func TestClaudeExecutorOmitsModelFlagWhenUnset(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if strings.Contains(result.Output, "--model") {
		t.Errorf("expected no --model flag when model unset, got: %s", result.Output)
	}
}

func TestClaudeExecutorPassesExtraArgs(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := ClaudeConfig{ExtraArgs: []string{"--allowedTools", "Read,Edit"}}
	result := NewClaudeExecutor(cfg)(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "--allowedTools Read,Edit") {
		t.Errorf("expected extra args passed through, got: %s", result.Output)
	}
}
