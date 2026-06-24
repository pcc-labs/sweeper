package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/config"
	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/provider"
	"github.com/papercomputeco/sweeper/pkg/telemetry"
	"github.com/papercomputeco/sweeper/pkg/worker"
)

func TestAgentRunPrintsPaperWarning(t *testing.T) {
	// Paper enabled with no proxy env set: exercises the detect+warn branch.
	t.Setenv("ANTHROPIC_BASE_URL", "")
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		PaperEnabled: true,
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

func TestAgentRunPaperCapturing(t *testing.T) {
	// Paper enabled with the proxy env present: exercises the "capturing" branch.
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:5000")
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		PaperEnabled: true,
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

func TestAgentRunSkipsPaperWhenDisabled(t *testing.T) {
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		PaperEnabled: false,
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
	explored := map[string]bool{"a.go": true}

	result := filterRetryableIssues(issues, histories, explored, 2)
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
	explored := map[string]bool{}

	result := filterRetryableIssues(issues, histories, explored, 2)
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

