package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/config"
	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/provider"
	"github.com/papercomputeco/sweeper/pkg/telemetry"
	"github.com/papercomputeco/sweeper/pkg/worker"
)

func TestAgentRunWithFakeExecutor(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		TargetDir:    dir,
		Concurrency:  2,
		TelemetryDir: t.TempDir(),
	}
	fakeIssues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "comment missing"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: fakeIssues, Parsed: true}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalIssues != 1 {
		t.Errorf("expected 1 total issue, got %d", summary.TotalIssues)
	}
	if summary.Fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", summary.Fixed)
	}
}

func TestAgentRunRawFallback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		TargetDir:    dir,
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		LinterName:   "custom",
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{
			RawOutput: "ERROR: something unparseable\n  details here\n",
			Parsed:    false,
		}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		if task.RawOutput == "" {
			t.Error("expected RawOutput to be set on task")
		}
		return worker.Result{TaskID: task.ID, Success: true, IssuesFix: 1}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalIssues != 1 {
		t.Errorf("expected 1 total issue, got %d", summary.TotalIssues)
	}
	if summary.Fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", summary.Fixed)
	}
}

func TestAgentRunRawDryRun(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		DryRun:       true,
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{
			RawOutput: "some raw output",
			Parsed:    false,
		}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Tasks != 1 {
		t.Errorf("expected 1 task in dry run, got %d", summary.Tasks)
	}
}

func TestAgentRunLinterError(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{}, errors.New("linter broke")
	}
	a := New(cfg, WithLinterFunc(fakeLinter))
	_, err := a.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from linter")
	}
}

func TestAgentRunParsedWithFailure(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{
			Issues: []linter.Issue{
				{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
			},
			Parsed: true,
		}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: false, Error: "agent failed"}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", summary.Failed)
	}
}

func TestAgentRunRawWithFailure(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{
			RawOutput: "unparseable output",
			Parsed:    false,
		}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, Success: false, Error: "agent failed"}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", summary.Failed)
	}
}

func TestAgentRunParsedDryRun(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		DryRun:       true,
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{
			Issues: []linter.Issue{
				{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
				{File: "b.go", Line: 2, Linter: "revive", Message: "msg"},
			},
			Parsed: true,
		}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalIssues != 2 {
		t.Errorf("expected 2 total issues, got %d", summary.TotalIssues)
	}
	if summary.Tasks != 2 {
		t.Errorf("expected 2 tasks, got %d", summary.Tasks)
	}
}

func TestDefaultLinterFunc(t *testing.T) {
	// Exercise defaultLinterFunc for coverage. It shells out to golangci-lint
	// which may or may not be installed; we don't care about the result.
	_, _ = defaultLinterFunc(context.Background(), t.TempDir())
}

func TestAgentRunMultiRoundAllFixed(t *testing.T) {
	callCount := 0
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 1 {
			return linter.ParseResult{Issues: issues, Parsed: true}, nil
		}
		// Re-lint: all clean
		return linter.ParseResult{Parsed: true}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1, Output: "fixed it"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      3,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", summary.Fixed)
	}
	if summary.Rounds != 1 {
		t.Errorf("expected 1 round, got %d", summary.Rounds)
	}
}

func TestAgentRunMultiRoundWithRetry(t *testing.T) {
	callCount := 0
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg1"},
		{File: "a.go", Line: 5, Linter: "revive", Message: "msg2"},
	}
	reducedIssues := []linter.Issue{
		{File: "a.go", Line: 5, Linter: "revive", Message: "msg2"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		switch callCount {
		case 1:
			return linter.ParseResult{Issues: issues, Parsed: true}, nil
		case 2:
			return linter.ParseResult{Issues: reducedIssues, Parsed: true}, nil
		default:
			return linter.ParseResult{Parsed: true}, nil
		}
	}
	var gotRetryPrompt bool
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		if strings.Contains(task.Prompt, "previous attempt") {
			gotRetryPrompt = true
		}
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: len(task.Issues), Output: "attempt output"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      3,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !gotRetryPrompt {
		t.Error("expected retry prompt on round 2")
	}
	if summary.Rounds != 2 {
		t.Errorf("expected 2 rounds, got %d", summary.Rounds)
	}
	if summary.Fixed != 2 {
		t.Errorf("expected 2 fixed, got %d", summary.Fixed)
	}
}

func TestAgentRunStagnationTriggersExploration(t *testing.T) {
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	var gotExplorationPrompt bool
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		if strings.Contains(task.Prompt, "Previous approaches have not resolved") {
			gotExplorationPrompt = true
		}
		return worker.Result{TaskID: task.ID, File: task.File, Success: false, Error: "failed", Output: "nope"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      5,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !gotExplorationPrompt {
		t.Error("expected exploration prompt after stagnation")
	}
	// After exploration fails, the file should be dropped from retries
	if summary.Rounds > 4 {
		t.Errorf("expected loop to stop after exploration, got %d rounds", summary.Rounds)
	}
}

func TestAgentRunReLintError(t *testing.T) {
	callCount := 0
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 1 {
			return linter.ParseResult{Issues: issues, Parsed: true}, nil
		}
		return linter.ParseResult{}, errors.New("relint broke")
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      3,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal("should not return error on re-lint failure")
	}
	if summary.Fixed != 1 {
		t.Errorf("expected 1 fixed from round 1, got %d", summary.Fixed)
	}
}

func TestAgentRunBackoffRespectsContextCancel(t *testing.T) {
	callCount := 0
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 2 {
			// Cancel context right before backoff would start
			cancel()
		}
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1, Output: "ok"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      3,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	// Should complete quickly despite backoff because context is cancelled
	_, _ = a.Run(ctx)
}

func TestAgentRunBackoffCapsAt60s(t *testing.T) {
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 1 {
			return linter.ParseResult{Issues: issues, Parsed: true}, nil
		}
		cancel() // cancel so backoff waits are instant
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: false, Error: "nope", Output: "nope"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      6,
		StaleThreshold: 99, // prevent exploration so all rounds run
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, _ := a.Run(ctx)
	// Should reach enough rounds to trigger the 60s cap (round 4: 5<<4=80 > 60)
	if summary.Rounds < 5 {
		t.Errorf("expected at least 5 rounds to exercise backoff cap, got %d", summary.Rounds)
	}
}

func TestAgentRunDryRunSkipsLoop(t *testing.T) {
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		DryRun:         true,
		MaxRounds:      3,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Rounds != 1 {
		t.Errorf("dry run should report 1 round, got %d", summary.Rounds)
	}
}

func TestFilterRetryableIssues(t *testing.T) {
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Message: "msg"},
		{File: "b.go", Line: 2, Message: "msg"},
	}
	histories := map[string]*loop.FileHistory{
		"a.go": {File: "a.go", Rounds: []loop.RoundResult{{Fixed: 0}, {Fixed: 0}}},
	}
	explored := map[string]int{"a.go": 0}

	result := filterRetryableIssues(issues, histories, explored, 2, nil)
	if len(result) != 1 {
		t.Errorf("expected 1 retryable issue, got %d", len(result))
	}
	if result[0].File != "b.go" {
		t.Errorf("expected b.go to be retryable, got %s", result[0].File)
	}
}

func TestFilterRetryableIssuesNoExploration(t *testing.T) {
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Message: "msg"},
	}
	histories := map[string]*loop.FileHistory{
		"a.go": {File: "a.go", Rounds: []loop.RoundResult{{Fixed: 0}, {Fixed: 0}}},
	}
	explored := map[string]int{}

	result := filterRetryableIssues(issues, histories, explored, 2, nil)
	if len(result) != 1 {
		t.Errorf("expected 1 retryable issue (exploration not tried), got %d", len(result))
	}
}

func TestSafeHistoryNil(t *testing.T) {
	fh := safeHistory(nil)
	if fh.File != "" {
		t.Error("nil should produce zero-value FileHistory")
	}
}

func TestSafeHistoryNonNil(t *testing.T) {
	fh := safeHistory(&loop.FileHistory{File: "test.go"})
	if fh.File != "test.go" {
		t.Errorf("expected test.go, got %s", fh.File)
	}
}

func TestAgentRunReLintErrorWithFailedExecutor(t *testing.T) {
	callCount := 0
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 1 {
			return linter.ParseResult{Issues: issues, Parsed: true}, nil
		}
		return linter.ParseResult{}, errors.New("relint broke")
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: false, Error: "nope"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      3,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal("should not return error on re-lint failure")
	}
	if summary.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", summary.Failed)
	}
}

func TestAgentRunMoreIssuesAfterFix(t *testing.T) {
	callCount := 0
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	moreIssues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
		{File: "a.go", Line: 5, Linter: "revive", Message: "new msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 1 {
			return linter.ParseResult{Issues: issues, Parsed: true}, nil
		}
		// Re-lint shows more issues than before (fix introduced new ones)
		return linter.ParseResult{Issues: moreIssues, Parsed: true}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1, Output: "tried"}
	}
	cfg := config.Config{
		TargetDir:      t.TempDir(),
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      2,
		StaleThreshold: 2,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Round 1: 1 issue before, 2 after -> fixed clamped to 0
	// Round 2 (last round): executor reports success
	if summary.Fixed < 0 {
		t.Errorf("fixed should never be negative, got %d", summary.Fixed)
	}
}

func TestAgentRunMaxRoundsZero(t *testing.T) {
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "msg"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	fakeExecutor := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		MaxRounds:    0,
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(fakeExecutor))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Rounds != 1 {
		t.Errorf("MaxRounds=0 should default to 1 round, got %d", summary.Rounds)
	}
}

type fakeVMForAgent struct {
	shutdownFn func() error
}

func (f *fakeVMForAgent) Shutdown() error {
	if f.shutdownFn != nil {
		return f.shutdownFn()
	}
	return nil
}

func TestAgentWithVMOption(t *testing.T) {
	var shutdownCalled bool
	fakeVM := &fakeVMForAgent{
		shutdownFn: func() error {
			shutdownCalled = true
			return nil
		},
	}
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		VM:           true,
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithVM(fakeVM))
	_, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !shutdownCalled {
		t.Error("expected VM.Shutdown() to be called via defer")
	}
}

func TestAgentWithVMDryRun(t *testing.T) {
	var shutdownCalled bool
	fakeVM := &fakeVMForAgent{
		shutdownFn: func() error {
			shutdownCalled = true
			return nil
		},
	}
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		VM:           true,
		DryRun:       true,
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{
			Issues: []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "msg"}},
			Parsed: true,
		}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithVM(fakeVM))
	_, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !shutdownCalled {
		t.Error("VM.Shutdown() should fire even in dry-run mode")
	}
}

func TestAgentWithVMContextCancel(t *testing.T) {
	var shutdownCalled bool
	fakeVM := &fakeVMForAgent{
		shutdownFn: func() error {
			shutdownCalled = true
			return nil
		},
	}
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		VM:           true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to simulate SIGINT
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{}, ctx.Err()
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithVM(fakeVM))
	_, _ = a.Run(ctx) // May return error from cancelled context
	if !shutdownCalled {
		t.Error("VM.Shutdown() should fire on context cancellation")
	}
}

func TestAgentRunWithCustomLintCommand(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		LintCommand:  []string{"eslint", "--fix", "."},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter))
	_, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunSessionDocError(t *testing.T) {
	// Use a target dir that is a file (not a directory) so .sweeper creation fails.
	tmp := t.TempDir()
	blocker := tmp + "/.sweeper"
	if err := os.WriteFile(blocker, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		TargetDir:    tmp,
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter))
	// Should not fail, just warn
	_, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildPromptCLIDefault(t *testing.T) {
	a := &Agent{providerKind: provider.KindCLI}
	task := worker.Task{
		File:   "main.go",
		Dir:    t.TempDir(),
		Issues: []linter.Issue{{File: "main.go", Line: 1, Message: "unused", Linter: "revive"}},
	}
	got := a.buildPrompt(task, loop.StrategyStandard, "")
	if !strings.Contains(got, "main.go") {
		t.Error("expected file reference")
	}
	// CLI default should NOT contain "unified diff" instructions
	if strings.Contains(got, "unified diff") {
		t.Error("CLI prompt should not ask for unified diff")
	}
}

func TestBuildPromptCLIRetry(t *testing.T) {
	a := &Agent{providerKind: provider.KindCLI}
	task := worker.Task{
		File:   "main.go",
		Dir:    t.TempDir(),
		Issues: []linter.Issue{{File: "main.go", Line: 1, Message: "unused", Linter: "revive"}},
	}
	got := a.buildPrompt(task, loop.StrategyRetry, "prior output")
	if !strings.Contains(got, "different approach") {
		t.Error("expected retry instructions")
	}
}

func TestBuildPromptCLIExploration(t *testing.T) {
	a := &Agent{providerKind: provider.KindCLI}
	task := worker.Task{
		File:   "main.go",
		Dir:    t.TempDir(),
		Issues: []linter.Issue{{File: "main.go", Line: 1, Message: "unused", Linter: "revive"}},
	}
	got := a.buildPrompt(task, loop.StrategyExploration, "prior output")
	if !strings.Contains(got, "refactoring") {
		t.Error("expected exploration instructions")
	}
}

func TestBuildPromptAPIDefault(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/main.go", []byte("package main\n"), 0o644)
	a := &Agent{providerKind: provider.KindAPI}
	task := worker.Task{
		File:   "main.go",
		Dir:    dir,
		Issues: []linter.Issue{{File: "main.go", Line: 1, Message: "unused", Linter: "revive"}},
	}
	got := a.buildPrompt(task, loop.StrategyStandard, "")
	if !strings.Contains(got, "unified diff") {
		t.Error("API prompt should ask for unified diff")
	}
	if !strings.Contains(got, "package main") {
		t.Error("API prompt should include file content")
	}
}

func TestBuildPromptAPIRetry(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/main.go", []byte("package main\n"), 0o644)
	a := &Agent{providerKind: provider.KindAPI}
	task := worker.Task{
		File:   "main.go",
		Dir:    dir,
		Issues: []linter.Issue{{File: "main.go", Line: 1, Message: "unused", Linter: "revive"}},
	}
	got := a.buildPrompt(task, loop.StrategyRetry, "prior output")
	if !strings.Contains(got, "different approach") {
		t.Error("expected retry instructions")
	}
	if !strings.Contains(got, "unified diff") {
		t.Error("API retry prompt should ask for unified diff")
	}
}

func TestBuildPromptAPIExploration(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/main.go", []byte("package main\n"), 0o644)
	a := &Agent{providerKind: provider.KindAPI}
	task := worker.Task{
		File:   "main.go",
		Dir:    dir,
		Issues: []linter.Issue{{File: "main.go", Line: 1, Message: "unused", Linter: "revive"}},
	}
	got := a.buildPrompt(task, loop.StrategyExploration, "prior output")
	if !strings.Contains(got, "refactoring") {
		t.Error("expected exploration instructions")
	}
	if !strings.Contains(got, "unified diff") {
		t.Error("API exploration prompt should ask for unified diff")
	}
}

func TestNewAgentFallbackOnUnknownProvider(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		Provider:     "nonexistent-provider-xyz",
	}
	a := New(cfg)
	// Should fall back to KindCLI and Claude executor without panicking
	if a.providerKind != provider.KindCLI {
		t.Errorf("expected KindCLI fallback, got %d", a.providerKind)
	}
	if a.executor == nil {
		t.Error("executor should not be nil")
	}
}

func TestNewAgentWithProviderFromRegistry(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		Provider:     "claude",
	}
	a := New(cfg)
	if a.providerKind != provider.KindCLI {
		t.Errorf("expected KindCLI for claude, got %d", a.providerKind)
	}
}

func TestNewAgentEmptyProviderDefaultsToClaude(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		Provider:     "",
	}
	a := New(cfg)
	if a.providerKind != provider.KindCLI {
		t.Errorf("expected KindCLI for empty provider, got %d", a.providerKind)
	}
}

func TestWithPublisherOption(t *testing.T) {
	fakePub := &fakePublisher{}
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{}, nil
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithPublisher(fakePub))
	_, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fakePub.publishCount == 0 {
		t.Error("expected WithPublisher to override default publisher and receive Publish calls")
	}
	if !fakePub.closed {
		t.Error("expected Close() to be called on publisher")
	}
}

type fakePublisher struct {
	publishCount int
	closed       bool
}

func (f *fakePublisher) Publish(_ context.Context, _ telemetry.Event) error {
	f.publishCount++
	return nil
}

func (f *fakePublisher) Close() error {
	f.closed = true
	return nil
}

type capturePublisher struct {
	events []telemetry.Event
}

func (p *capturePublisher) Publish(ctx context.Context, e telemetry.Event) error {
	p.events = append(p.events, e)
	return nil
}
func (p *capturePublisher) Close() error { return nil }

// advisorAgentConfig returns a config with two files' worth of issues and
// concurrency 1 so dispatch order is observable.
func advisorAgentTestSetup() (config.Config, LinterFunc) {
	cfg := config.Config{
		TargetDir:    ".",
		Concurrency:  1,
		TelemetryDir: "",
	}
	issues := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "m1"},
		{File: "b.go", Line: 2, Linter: "revive", Message: "m2"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	return cfg, fakeLinter
}

func TestAgentAdvisorReordersDispatch(t *testing.T) {
	cfg, fakeLinter := advisorAgentTestSetup()
	cfg.TelemetryDir = t.TempDir()

	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: true,
			Output: `{"tasks":[{"file":"b.go"},{"file":"a.go"}]}`}
	}
	var mu sync.Mutex
	ids := make(map[string]int)
	workerExec := func(ctx context.Context, task worker.Task) worker.Result {
		mu.Lock()
		ids[task.File] = task.ID
		mu.Unlock()
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(workerExec), WithAdvisorExecutor(advisorExec))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// worker.Pool.RunStream spawns all task goroutines immediately and lets
	// them race for the semaphore, so execution order is nondeterministic
	// even at Concurrency 1. What is deterministic is the worker.Task.ID
	// assigned by position in the advisor-reordered slice: since the
	// advisor moved b.go before a.go, b.go must get ID 0 and a.go ID 1.
	mu.Lock()
	defer mu.Unlock()
	if ids["b.go"] != 0 || ids["a.go"] != 1 {
		t.Errorf("expected advisor reorder to assign b.go=0 a.go=1, got %v", ids)
	}
}

func TestAgentAdvisorFallbackOnGarbage(t *testing.T) {
	cfg, fakeLinter := advisorAgentTestSetup()
	cfg.TelemetryDir = t.TempDir()

	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: true, Output: "not json"}
	}
	var mu sync.Mutex
	ids := make(map[string]int)
	workerExec := func(ctx context.Context, task worker.Task) worker.Result {
		mu.Lock()
		ids[task.File] = task.ID
		mu.Unlock()
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(workerExec), WithAdvisorExecutor(advisorExec))
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// worker.Pool.RunStream races all tasks for the semaphore, so execution
	// order is nondeterministic. What's deterministic is the mechanical
	// (sorted-by-file) plan's assignment of worker.Task.ID: a.go=0, b.go=1.
	mu.Lock()
	defer mu.Unlock()
	if ids["a.go"] != 0 || ids["b.go"] != 1 {
		t.Errorf("expected mechanical fallback to assign a.go=0 b.go=1, got %v", ids)
	}
	if summary.Fixed != 2 {
		t.Errorf("expected 2 fixed despite advisor failure, got %d", summary.Fixed)
	}
}

func TestAgentAdvisorPublishesPlanEvent(t *testing.T) {
	cfg, fakeLinter := advisorAgentTestSetup()
	cfg.TelemetryDir = t.TempDir()

	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: true,
			Output: `{"tasks":[{"file":"a.go"},{"file":"b.go"}]}`}
	}
	workerExec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	pub := &capturePublisher{}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(workerExec),
		WithAdvisorExecutor(advisorExec), WithPublisher(pub))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var planEvents []telemetry.Event
	for _, e := range pub.events {
		if e.Type == "advisor_plan" {
			planEvents = append(planEvents, e)
		}
	}
	if len(planEvents) != 1 {
		t.Fatalf("expected 1 advisor_plan event, got %d", len(planEvents))
	}
	d := planEvents[0].Data
	if d["success"] != true {
		t.Errorf("expected success=true, got %v", d["success"])
	}
	if d["files_input"] != 2 || d["files_planned"] != 2 {
		t.Errorf("expected files_input=2 files_planned=2, got %v / %v", d["files_input"], d["files_planned"])
	}
}

func TestAgentAdvisorStrategyHintUsed(t *testing.T) {
	// Two rounds; worker never fixes; stale-threshold 99 so the mechanical
	// pick can never reach exploration. Round 2's advisor hint must be the
	// only way the exploration prompt appears.
	dir := t.TempDir()
	cfg := config.Config{
		TargetDir:      dir,
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      2,
		StaleThreshold: 99,
	}
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m1"}}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	round := 0
	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		round++
		strategy := "standard"
		if round > 1 {
			strategy = "exploration"
		}
		return worker.Result{Success: true,
			Output: `{"tasks":[{"file":"a.go","strategy":"` + strategy + `"}]}`}
	}
	var prompts []string
	workerExec := func(ctx context.Context, task worker.Task) worker.Result {
		prompts = append(prompts, task.Prompt)
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 0}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(workerExec), WithAdvisorExecutor(advisorExec))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 worker prompts, got %d", len(prompts))
	}
	if strings.Contains(prompts[0], "Consider refactoring") {
		t.Error("round 1 should be standard, not exploration")
	}
	if !strings.Contains(prompts[1], "Consider refactoring") {
		t.Error("round 2 advisor hint should produce the exploration prompt")
	}
}

func TestAgentAdvisorSkippedInDryRun(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		DryRun:       true,
	}
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m1"}}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	advisorCalled := false
	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		advisorCalled = true
		return worker.Result{Success: true, Output: `{"tasks":[{"file":"a.go"}]}`}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithAdvisorExecutor(advisorExec))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if advisorCalled {
		t.Error("expected advisor not to run under --dry-run")
	}
}

func TestNewAgentAdvisorRequiresCLIProvider(t *testing.T) {
	cfg := config.Config{
		TargetDir:       t.TempDir(),
		Concurrency:     1,
		TelemetryDir:    t.TempDir(),
		Provider:        "claude",
		AdvisorProvider: "ollama", // KindAPI — cannot advise
	}
	a := New(cfg)
	if a.advisorExec != nil {
		t.Error("expected advisor disabled for KindAPI provider")
	}
}

func TestNewAgentAdvisorModelOnlyDefaultsToClaude(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		Provider:     "claude",
		AdvisorModel: "claude-opus-4-8",
	}
	a := New(cfg)
	if a.advisorExec == nil {
		t.Error("expected advisor enabled when only model is set (provider defaults to claude)")
	}
}

func TestNewAgentAdvisorDisabledInVMMode(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		Provider:     "claude",
		AdvisorModel: "claude-opus-4-8",
		VM:           true,
	}
	a := New(cfg)
	if a.advisorExec != nil {
		t.Error("expected advisor disabled in VM mode")
	}
}

func TestNewAgentBuildsLadderFromConfig(t *testing.T) {
	cfg := config.Config{
		TargetDir:        t.TempDir(),
		Concurrency:      1,
		TelemetryDir:     t.TempDir(),
		Provider:         "claude",
		EscalationLadder: []string{"claude-haiku-4-5", "claude/claude-sonnet-5"},
	}
	a := New(cfg)
	if len(a.ladder) != 2 {
		t.Fatalf("expected 2 rungs, got %d", len(a.ladder))
	}
	if a.ladder[0].Provider != "claude" || a.ladder[0].Model != "claude-haiku-4-5" {
		t.Errorf("unexpected rung 1: %+v", a.ladder[0])
	}
	if a.ladder[1].Provider != "claude" || a.ladder[1].Model != "claude-sonnet-5" {
		t.Errorf("unexpected rung 2: %+v", a.ladder[1])
	}
	if a.ladder[0].Kind != provider.KindCLI {
		t.Errorf("expected KindCLI rung, got %v", a.ladder[0].Kind)
	}
	if a.ladder[0].Exec == nil {
		t.Error("expected rung executor constructed")
	}
}

func TestNewAgentLadderDisabledInVMMode(t *testing.T) {
	cfg := config.Config{
		TargetDir:        t.TempDir(),
		Concurrency:      1,
		TelemetryDir:     t.TempDir(),
		Provider:         "claude",
		EscalationLadder: []string{"claude-haiku-4-5"},
		VM:               true,
	}
	a := New(cfg)
	if a.ladder != nil {
		t.Error("expected ladder disabled in VM mode")
	}
}

func TestNewAgentLadderEmptyEntryDisablesLadder(t *testing.T) {
	cfg := config.Config{
		TargetDir:        t.TempDir(),
		Concurrency:      1,
		TelemetryDir:     t.TempDir(),
		Provider:         "claude",
		EscalationLadder: []string{"claude-haiku-4-5", "  "},
	}
	a := New(cfg)
	if a.ladder != nil {
		t.Error("expected ladder disabled when an entry is blank")
	}
}

func TestRungExecutorZeroIsBase(t *testing.T) {
	base := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{File: "base"}
	}
	rung1 := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{File: "rung1"}
	}
	cfg := config.Config{TargetDir: t.TempDir(), Concurrency: 1, TelemetryDir: t.TempDir()}
	a := New(cfg, WithExecutor(base), WithLadder([]LadderRung{{Exec: rung1, Kind: provider.KindCLI, Provider: "claude", Model: "m1"}}))
	if got := a.rungExecutor(0)(context.Background(), worker.Task{}); got.File != "base" {
		t.Errorf("rung 0 should be the base executor, got %s", got.File)
	}
	if got := a.rungExecutor(1)(context.Background(), worker.Task{}); got.File != "rung1" {
		t.Errorf("rung 1 should be the ladder executor, got %s", got.File)
	}
}

// ladderTestRung returns a LadderRung whose executor records the files it
// handled into calls (mutex-guarded) and never fixes anything.
func ladderTestRung(t *testing.T, model string, calls *[]string, mu *sync.Mutex) LadderRung {
	t.Helper()
	return LadderRung{
		Kind:     provider.KindCLI,
		Provider: "claude",
		Model:    model,
		Exec: func(ctx context.Context, task worker.Task) worker.Result {
			mu.Lock()
			*calls = append(*calls, task.File)
			mu.Unlock()
			return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 0, Model: model}
		},
	}
}

func TestAgentLadderEscalatesOnStagnation(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		TargetDir:      dir,
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      2,
		StaleThreshold: 1,
	}
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m1"}}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	var mu sync.Mutex
	var baseCalls, rung1Calls []string
	var rung1Prompts []string
	base := func(ctx context.Context, task worker.Task) worker.Result {
		mu.Lock()
		baseCalls = append(baseCalls, task.File)
		mu.Unlock()
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 0}
	}
	rung1 := LadderRung{
		Kind: provider.KindCLI, Provider: "claude", Model: "claude-haiku-4-5",
		Exec: func(ctx context.Context, task worker.Task) worker.Result {
			mu.Lock()
			rung1Calls = append(rung1Calls, task.File)
			rung1Prompts = append(rung1Prompts, task.Prompt)
			mu.Unlock()
			return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 0, Model: "claude-haiku-4-5"}
		},
	}
	pub := &capturePublisher{}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(base), WithLadder([]LadderRung{rung1}), WithPublisher(pub))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Round 1: base rung, standard. Round 2: stagnant -> exploration + climb.
	if len(baseCalls) != 1 || baseCalls[0] != "a.go" {
		t.Errorf("expected round 1 on base, got %v", baseCalls)
	}
	if len(rung1Calls) != 1 || rung1Calls[0] != "a.go" {
		t.Errorf("expected round 2 escalated to rung 1, got %v", rung1Calls)
	}
	if len(rung1Prompts) != 1 || !strings.Contains(rung1Prompts[0], "Consider refactoring") {
		t.Error("expected exploration prompt kept on the escalated attempt")
	}
	// Telemetry: rung 0 then rung 1.
	var rungs []int
	for _, e := range pub.events {
		if e.Type == "fix_attempt" {
			if r, ok := e.Data["rung"].(int); ok {
				rungs = append(rungs, r)
			}
		}
	}
	if len(rungs) != 2 || rungs[0] != 0 || rungs[1] != 1 {
		t.Errorf("expected fix_attempt rungs [0 1], got %v", rungs)
	}
}

func TestAgentLadderKeepsFileWhileRungsRemain(t *testing.T) {
	// With a 2-rung ladder and permanent stagnation, the file must survive
	// exploration at rung 1 and get a rung-2 attempt before being dropped.
	dir := t.TempDir()
	cfg := config.Config{
		TargetDir:      dir,
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      3,
		StaleThreshold: 1,
	}
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m1"}}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	var mu sync.Mutex
	var baseCalls, rung1Calls, rung2Calls []string
	base := func(ctx context.Context, task worker.Task) worker.Result {
		mu.Lock()
		baseCalls = append(baseCalls, task.File)
		mu.Unlock()
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 0}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(base), WithLadder([]LadderRung{
		ladderTestRung(t, "m1", &rung1Calls, &mu),
		ladderTestRung(t, "m2", &rung2Calls, &mu),
	}))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(baseCalls) != 1 || len(rung1Calls) != 1 || len(rung2Calls) != 1 {
		t.Errorf("expected one attempt per rung [1 1 1], got [%d %d %d]",
			len(baseCalls), len(rung1Calls), len(rung2Calls))
	}
}

func TestFilterRetryableKeepsStagnantFileBelowTop(t *testing.T) {
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m"}}
	histories := map[string]*loop.FileHistory{
		"a.go": {File: "a.go", Rounds: []loop.RoundResult{{Fixed: 0}, {Fixed: 0}}},
	}
	esc := loop.NewEscalation(2)
	rung := esc.Bump("a.go") // rung 1 of 2 — one rung left; exploration dispatched here
	explorationRungs := map[string]int{"a.go": rung}
	got := filterRetryableIssues(issues, histories, explorationRungs, 1, esc)
	if len(got) != 1 {
		t.Errorf("expected stagnant file kept while below top rung, got %d issues", len(got))
	}
	rung = esc.Bump("a.go") // now at top; exploration dispatched at the top rung
	explorationRungs["a.go"] = rung
	got = filterRetryableIssues(issues, histories, explorationRungs, 1, esc)
	if len(got) != 0 {
		t.Errorf("expected stagnant file dropped at top rung, got %d issues", len(got))
	}
}

// TestFilterRetryableKeepsFileSeededToTopWithoutTopRungExploration is the
// RED case for the exploration-tracking bug: a file explores at a LOW rung,
// is later seeded straight to the TOP rung (e.g. by an advisor tier hint)
// without exploration ever having been dispatched there, and goes stagnant.
// The documented invariant is "the file survives until exploration has been
// attempted at the TOP rung" — tracking only the file's current rung (as
// esc.AtTop did) would wrongly drop it the moment the seed lands on top.
func TestFilterRetryableKeepsFileSeededToTopWithoutTopRungExploration(t *testing.T) {
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m"}}
	histories := map[string]*loop.FileHistory{
		"a.go": {File: "a.go", Rounds: []loop.RoundResult{{Fixed: 0}, {Fixed: 0}}},
	}
	esc := loop.NewEscalation(2)
	lowRung := esc.Bump("a.go") // exploration dispatched at rung 1 only
	explorationRungs := map[string]int{"a.go": lowRung}

	esc.Seed("a.go", 2) // advisor tier hint seeds straight to the top rung;
	// no exploration has run there yet.

	got := filterRetryableIssues(issues, histories, explorationRungs, 1, esc)
	if len(got) != 1 {
		t.Errorf("expected file kept: exploration never ran at the top rung (esc.Rung=%d, recorded=%d), got %d issues",
			esc.Rung("a.go"), explorationRungs["a.go"], len(got))
	}

	// Once exploration actually runs at the top rung, the file may be dropped.
	explorationRungs["a.go"] = esc.Top()
	got = filterRetryableIssues(issues, histories, explorationRungs, 1, esc)
	if len(got) != 0 {
		t.Errorf("expected file dropped once exploration has run at the top rung, got %d issues", len(got))
	}
}

func TestFilterRetryableNilEscalationMatchesOldBehavior(t *testing.T) {
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m"}}
	histories := map[string]*loop.FileHistory{
		"a.go": {File: "a.go", Rounds: []loop.RoundResult{{Fixed: 0}, {Fixed: 0}}},
	}
	explorationRungs := map[string]int{"a.go": 0}
	got := filterRetryableIssues(issues, histories, explorationRungs, 1, nil)
	if len(got) != 0 {
		t.Errorf("expected old drop behavior with nil escalation, got %d issues", len(got))
	}
}

func TestAgentAdvisorTierSeedsStartingRung(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m1"}}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: true,
			Output: `{"tasks":[{"file":"a.go","tier":"claude-sonnet-5"}]}`}
	}
	var mu sync.Mutex
	var baseCalls, rung1Calls, rung2Calls []string
	base := func(ctx context.Context, task worker.Task) worker.Result {
		mu.Lock()
		baseCalls = append(baseCalls, task.File)
		mu.Unlock()
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(base),
		WithAdvisorExecutor(advisorExec),
		WithLadder([]LadderRung{
			ladderTestRung(t, "claude-haiku-4-5", &rung1Calls, &mu),
			ladderTestRung(t, "claude-sonnet-5", &rung2Calls, &mu),
		}))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Tier hint names rung 2's model, so round 1 dispatches there directly.
	if len(rung2Calls) != 1 || rung2Calls[0] != "a.go" {
		t.Errorf("expected round 1 seeded onto rung 2, got base=%v rung1=%v rung2=%v",
			baseCalls, rung1Calls, rung2Calls)
	}
}

func TestAgentAdvisorUnknownTierIgnored(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
	}
	issues := []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "m1"}}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		return linter.ParseResult{Issues: issues, Parsed: true}, nil
	}
	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: true,
			Output: `{"tasks":[{"file":"a.go","tier":"gpt-42-ultra"}]}`}
	}
	var mu sync.Mutex
	var baseCalls, rung1Calls []string
	base := func(ctx context.Context, task worker.Task) worker.Result {
		mu.Lock()
		baseCalls = append(baseCalls, task.File)
		mu.Unlock()
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(base),
		WithAdvisorExecutor(advisorExec),
		WithLadder([]LadderRung{ladderTestRung(t, "claude-haiku-4-5", &rung1Calls, &mu)}))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(baseCalls) != 1 || len(rung1Calls) != 0 {
		t.Errorf("expected unknown tier ignored (base dispatch), got base=%v rung1=%v", baseCalls, rung1Calls)
	}
}

// TestAdvisorExplorationHintDoesNotBumpWithoutStagnation is the RED case for
// the bump-gating bug: advisor.ResolveStrategy honors an "exploration" hint
// whenever history exists, with no stagnation check. Round 1 makes progress
// (Fixed>0, not stagnant); round 2's advisor hint forces "exploration"
// anyway. The exploration prompt may still be honored, but the ladder must
// NOT climb — escalation is stagnation-triggered, not hint-triggered.
func TestAdvisorExplorationHintDoesNotBumpWithoutStagnation(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		TargetDir:      dir,
		Concurrency:    1,
		TelemetryDir:   t.TempDir(),
		MaxRounds:      2,
		StaleThreshold: 2,
	}
	callCount := 0
	issuesRound1 := []linter.Issue{
		{File: "a.go", Line: 1, Linter: "revive", Message: "m1"},
		{File: "a.go", Line: 5, Linter: "revive", Message: "m2"},
	}
	issuesRound2 := []linter.Issue{
		{File: "a.go", Line: 5, Linter: "revive", Message: "m2"},
	}
	fakeLinter := func(ctx context.Context, dir string) (linter.ParseResult, error) {
		callCount++
		if callCount == 1 {
			return linter.ParseResult{Issues: issuesRound1, Parsed: true}, nil
		}
		return linter.ParseResult{Issues: issuesRound2, Parsed: true}, nil
	}
	advisorRound := 0
	advisorExec := func(ctx context.Context, task worker.Task) worker.Result {
		advisorRound++
		if advisorRound == 1 {
			return worker.Result{Success: true, Output: `{"tasks":[{"file":"a.go"}]}`}
		}
		// Round 2: advisor forces "exploration" even though round 1 made
		// progress (Fixed>0 => not stagnant).
		return worker.Result{Success: true, Output: `{"tasks":[{"file":"a.go","strategy":"exploration"}]}`}
	}
	base := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{TaskID: task.ID, File: task.File, Success: true, IssuesFix: 1}
	}
	pub := &capturePublisher{}
	a := New(cfg, WithLinterFunc(fakeLinter), WithExecutor(base),
		WithAdvisorExecutor(advisorExec),
		WithLadder([]LadderRung{{Kind: provider.KindCLI, Provider: "claude", Model: "m1", Exec: base}}),
		WithPublisher(pub))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rungs []int
	for _, e := range pub.events {
		if e.Type == "fix_attempt" {
			if r, ok := e.Data["rung"].(int); ok {
				rungs = append(rungs, r)
			}
		}
	}
	if len(rungs) != 2 {
		t.Fatalf("expected 2 fix_attempt events, got %d: %v", len(rungs), rungs)
	}
	if rungs[1] != 0 {
		t.Errorf("expected round 2 rung to stay 0 (advisor exploration hint without stagnation must not bump), got %d", rungs[1])
	}
}
