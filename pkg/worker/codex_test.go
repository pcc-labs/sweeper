package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCodex installs a fake codex binary on PATH that echoes its args.
func fakeCodex(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestCodexExecutorPassesModelFlag(t *testing.T) {
	fakeCodex(t)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewCodexExecutor(CodexConfig{Model: "o4-mini"})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "--model o4-mini") {
		t.Errorf("expected --model flag passed to codex, got: %s", result.Output)
	}
	if result.Model != "o4-mini" {
		t.Errorf("expected result.Model recorded, got %q", result.Model)
	}
}

func TestCodexExecutorOmitsModelFlagWhenUnset(t *testing.T) {
	fakeCodex(t)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewCodexExecutor(CodexConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if strings.Contains(result.Output, "--model") {
		t.Errorf("expected no --model flag when model unset, got: %s", result.Output)
	}
}

func TestCodexExecutorPassesExtraArgs(t *testing.T) {
	fakeCodex(t)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := CodexConfig{ExtraArgs: []string{"--sandbox", "workspace-write"}}
	result := NewCodexExecutor(cfg)(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "--sandbox workspace-write") {
		t.Errorf("expected extra args passed through, got: %s", result.Output)
	}
}
