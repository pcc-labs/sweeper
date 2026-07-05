package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/linter"
)

type fakeVM struct {
	execFunc func(ctx context.Context, args ...string) ([]byte, error)
}

func (f *fakeVM) Exec(ctx context.Context, args ...string) ([]byte, error) {
	return f.execFunc(ctx, args...)
}

func TestNewVMExecutor(t *testing.T) {
	vm := &fakeVM{
		execFunc: func(ctx context.Context, args ...string) ([]byte, error) {
			return []byte("fixed"), nil
		},
	}
	exec := NewVMExecutor(vm, VMExecConfig{})
	task := Task{
		ID:   0,
		File: "src/main.go",
		Dir:  "/host/project",
		Issues: []linter.Issue{
			{File: "src/main.go", Line: 10, Message: "unused var", Linter: "revive"},
		},
		Prompt: "Fix the lint issues",
	}
	result := exec(context.Background(), task)
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.Output != "fixed" {
		t.Errorf("expected 'fixed', got %s", result.Output)
	}
	if result.IssuesFix != 1 {
		t.Errorf("expected 1 issue fix, got %d", result.IssuesFix)
	}
	if result.Provider != "claude" {
		t.Errorf("expected provider claude, got %q", result.Provider)
	}
}

func TestNewVMExecutorError(t *testing.T) {
	vm := &fakeVM{
		execFunc: func(ctx context.Context, args ...string) ([]byte, error) {
			return []byte("error output"), fmt.Errorf("exit 1")
		},
	}
	exec := NewVMExecutor(vm, VMExecConfig{Model: "claude-haiku-4-5"})
	task := Task{
		ID:     0,
		File:   "main.go",
		Dir:    "/host/project",
		Prompt: "Fix it",
	}
	result := exec(context.Background(), task)
	if result.Success {
		t.Error("expected failure")
	}
	if result.Output != "error output" {
		t.Errorf("expected error output, got %s", result.Output)
	}
	if result.Provider != "claude" || result.Model != "claude-haiku-4-5" {
		t.Errorf("expected provider/model on error result, got %q/%q", result.Provider, result.Model)
	}
}

func TestNewVMExecutorModelFlag(t *testing.T) {
	var gotArgs []string
	vm := &fakeVM{
		execFunc: func(ctx context.Context, args ...string) ([]byte, error) {
			gotArgs = args
			return []byte("ok"), nil
		},
	}
	exec := NewVMExecutor(vm, VMExecConfig{Model: "claude-sonnet-5"})
	result := exec(context.Background(), Task{ID: 1, File: "a.go", Prompt: "fix"})
	want := []string{"claude", "--print", "--dangerously-skip-permissions", "--model", "claude-sonnet-5", "fix"}
	if len(gotArgs) != len(want) {
		t.Fatalf("expected args %v, got %v", want, gotArgs)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Errorf("arg[%d]: expected %q, got %q", i, want[i], gotArgs[i])
		}
	}
	if result.Model != "claude-sonnet-5" {
		t.Errorf("expected result model claude-sonnet-5, got %q", result.Model)
	}
}

func TestNewVMExecutorOmitsModelFlagWhenEmpty(t *testing.T) {
	var gotArgs []string
	vm := &fakeVM{
		execFunc: func(ctx context.Context, args ...string) ([]byte, error) {
			gotArgs = args
			return []byte("ok"), nil
		},
	}
	exec := NewVMExecutor(vm, VMExecConfig{})
	exec(context.Background(), Task{ID: 1, File: "a.go", Prompt: "fix"})
	want := []string{"claude", "--print", "--dangerously-skip-permissions", "fix"}
	if len(gotArgs) != len(want) {
		t.Fatalf("expected args %v, got %v", want, gotArgs)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Errorf("arg[%d]: expected %q, got %q", i, want[i], gotArgs[i])
		}
	}
}
