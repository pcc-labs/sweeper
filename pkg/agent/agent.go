package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/papercomputeco/sweeper/pkg/config"
	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/paper"
	"github.com/papercomputeco/sweeper/pkg/planner"
	"github.com/papercomputeco/sweeper/pkg/provider"
	"github.com/papercomputeco/sweeper/pkg/session"
	"github.com/papercomputeco/sweeper/pkg/telemetry"
	"github.com/papercomputeco/sweeper/pkg/worker"
)

type LinterFunc func(ctx context.Context, dir string) (linter.ParseResult, error)

// VMManager is the interface for VM lifecycle management.
type VMManager interface {
	Shutdown() error
}

type Summary struct {
	TotalIssues int
	Tasks       int
	Fixed       int
	Failed      int
	Rounds      int
}

type Agent struct {
	cfg          config.Config
	linterFn     LinterFunc
	executor     worker.Executor
	providerKind provider.Kind
	pub          telemetry.Publisher
	vm           VMManager
	sessionPath  string
}

type Option func(*Agent)

func WithLinterFunc(fn LinterFunc) Option {
	return func(a *Agent) { a.linterFn = fn }
}

func WithExecutor(exec worker.Executor) Option {
	return func(a *Agent) { a.executor = exec }
}

func WithVM(vm VMManager) Option {
	return func(a *Agent) { a.vm = vm }
}

func WithPublisher(pub telemetry.Publisher) Option {
	return func(a *Agent) { a.pub = pub }
}

func defaultLinterFunc(ctx context.Context, dir string) (linter.ParseResult, error) {
	return linter.Run(ctx, dir)
}

func New(cfg config.Config, opts ...Option) *Agent {
	a := &Agent{
		cfg:      cfg,
		linterFn: defaultLinterFunc,
		pub:      telemetry.NewJSONLPublisher(cfg.TelemetryDir),
	}

	// Resolve provider from registry; fall back to Claude if lookup fails
	// (cmd/run.go validates before reaching here, so fallback is defensive).
	provName := cfg.Provider
	if provName == "" {
		provName = "claude"
	}
	if p, err := provider.Get(provName); err == nil {
		a.providerKind = p.Kind
		a.executor = p.NewExec(provider.Config{
			Model:   cfg.ProviderModel,
			APIBase: cfg.ProviderAPI,
		})
	} else {
		fmt.Printf("Warning: unknown provider %q, falling back to claude\n", provName)
		a.providerKind = provider.KindCLI
		a.executor = worker.NewClaudeExecutor()
	}

	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *Agent) Run(ctx context.Context) (Summary, error) {
	defer func() { _ = a.pub.Close() }()
	if a.vm != nil {
		defer func() { _ = a.vm.Shutdown() }()
	}

	// Paper capture works by the spawned `claude` child inheriting
	// ANTHROPIC_BASE_URL, so the detect+warn only applies to the claude provider.
	providerName := a.cfg.Provider
	if providerName == "" {
		providerName = "claude"
	}
	if a.cfg.PaperEnabled && providerName == "claude" {
		if s := paper.Check(); s.Enabled {
			fmt.Printf("Paper: capturing via %s\n", s.ProxyURL)
		} else if s.Message != "" {
			fmt.Printf("Warning: %s\n", s.Message)
		}
	}

	lintCmd := "golangci-lint run ./..."
	if len(a.cfg.LintCommand) > 0 {
		lintCmd = strings.Join(a.cfg.LintCommand, " ")
	}
	sessionCfg := session.Config{
		Objective:   "Fix lint issues",
		LintCommand: lintCmd,
		TargetDir:   a.cfg.TargetDir,
		MaxRounds:   a.cfg.MaxRounds,
	}
	sp, err := session.Generate(filepath.Join(a.cfg.TargetDir, ".sweeper"), sessionCfg)
	if err != nil {
		fmt.Printf("Warning: session doc: %v\n", err)
	} else {
		a.sessionPath = sp
		fmt.Printf("Session: %s\n", sp)
	}

	_ = a.pub.Publish(ctx, telemetry.Event{
		Timestamp: time.Now(),
		Type:      "init",
		Data: map[string]any{
			"name":           fmt.Sprintf("sweep-%s", time.Now().Format("2006-01-02")),
			"linterCommand":  sessionCfg.LintCommand,
			"targetDir":      a.cfg.TargetDir,
			"maxRounds":      a.cfg.MaxRounds,
			"staleThreshold": a.cfg.StaleThreshold,
		},
	})

	fmt.Println("Running linter...")
	result, err := a.linterFn(ctx, a.cfg.TargetDir)
	if err != nil {
		return Summary{}, fmt.Errorf("linting: %w", err)
	}

	linterName := a.cfg.LinterName
	if linterName == "" {
		linterName = "golangci-lint"
	}

	if result.Parsed {
		return a.runParsed(ctx, result, linterName)
	}
	if result.RawOutput != "" {
		return a.runRaw(ctx, result, linterName)
	}
	fmt.Println("No lint issues found.")
	return Summary{}, nil
}

func (a *Agent) runParsed(ctx context.Context, result linter.ParseResult, linterName string) (Summary, error) {
	issues := result.Issues
	fmt.Printf("Found %d lint issues across files.\n", len(issues))

	maxRounds := a.cfg.MaxRounds
	if maxRounds < 1 {
		maxRounds = 1
	}

	summary := Summary{TotalIssues: len(issues)}
	fileHistories := make(map[string]*loop.FileHistory)
	explorationAttempted := make(map[string]bool)

	for round := 0; round < maxRounds; round++ {
		if len(issues) == 0 {
			break
		}

		fixTasks := planner.GroupByFile(issues)
		tasks := make([]worker.Task, len(fixTasks))
		strategies := make([]loop.Strategy, len(fixTasks))

		for i, ft := range fixTasks {
			fh := safeHistory(fileHistories[ft.File])
			strategy := loop.PickStrategy(round, fh, a.cfg.StaleThreshold)
			strategies[i] = strategy

			tasks[i] = worker.Task{
				ID:     i,
				File:   ft.File,
				Dir:    a.cfg.TargetDir,
				Issues: ft.Issues,
			}
			tasks[i].Prompt = a.buildPrompt(tasks[i], strategy, fh.LastOutput())
			if strategy == loop.StrategyExploration {
				explorationAttempted[ft.File] = true
			}
		}

		if round == 0 {
			summary.Tasks = len(tasks)
			fmt.Printf("Created %d fix tasks.\n", len(tasks))
		} else {
			fmt.Printf("Round %d: %d files with remaining issues.\n", round+1, len(tasks))
		}

		if a.cfg.DryRun {
			fmt.Println("Dry run - would fix:")
			for _, t := range tasks {
				fmt.Printf("  - %s (%d issues)\n", t.File, len(t.Issues))
			}
			summary.Rounds = round + 1
			return summary, nil
		}

		results := a.runRound(ctx, tasks)
		summary.Rounds = round + 1

		for i, r := range results {
			strategy := strategies[i]
			a.publishFixAttempt(ctx, r, linterName, round, strategy)

			// Update file history
			fh, ok := fileHistories[r.File]
			if !ok {
				fh = &loop.FileHistory{File: r.File}
				fileHistories[r.File] = fh
			}
			fh.Rounds = append(fh.Rounds, loop.RoundResult{
				File:         r.File,
				Round:        round,
				Strategy:     strategy,
				IssuesBefore: len(tasks[i].Issues),
				Output:       r.Output,
				Success:      r.Success,
				Error:        r.Error,
			})
		}

		a.publishRoundComplete(ctx, round, linterName, len(tasks), results)

		// If last round, tally results and stop
		if round >= maxRounds-1 {
			for _, r := range results {
				if r.Success {
					summary.Fixed += r.IssuesFix
				} else {
					summary.Failed++
				}
			}
			break
		}

		// Re-lint to check remaining issues
		reResult, err := a.linterFn(ctx, a.cfg.TargetDir)
		if err != nil {
			// Don't fail the whole run; tally current results and stop
			for _, r := range results {
				if r.Success {
					summary.Fixed += r.IssuesFix
				} else {
					summary.Failed++
				}
			}
			break
		}

		// Count fixes from this round based on re-lint results
		remainingByFile := make(map[string]int)
		if reResult.Parsed {
			for _, iss := range reResult.Issues {
				remainingByFile[iss.File]++
			}
		}
		for i, r := range results {
			before := len(tasks[i].Issues)
			after := remainingByFile[r.File]
			fixed := before - after
			if fixed < 0 {
				fixed = 0
			}
			summary.Fixed += fixed

			// Update round result with actual fix counts
			fh := fileHistories[r.File]
			last := &fh.Rounds[len(fh.Rounds)-1]
			last.IssuesAfter = after
			last.Fixed = fixed
		}

		if !reResult.Parsed || len(reResult.Issues) == 0 {
			fmt.Println("All issues resolved!")
			if a.sessionPath != "" {
				_ = session.UpdateStatus(a.sessionPath, round+1, len(reResult.Issues), summary.Fixed, 0)
			}
			break
		}

		// Exponential backoff between rounds: 5s, 10s, 20s, ...
		backoff := time.Duration(5<<uint(round)) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
		fmt.Printf("Backoff: waiting %s before next round...\n", backoff)
		select {
		case <-ctx.Done():
			break
		case <-time.After(backoff):
		}

		// Filter to retryable issues
		issues = filterRetryableIssues(reResult.Issues, fileHistories, explorationAttempted, a.cfg.StaleThreshold)

		if a.sessionPath != "" {
			_ = session.UpdateStatus(a.sessionPath, round+1, len(reResult.Issues), summary.Fixed, len(issues))
		}
	}

	fmt.Printf("Results: %d fixed, %d failed (%d rounds).\n", summary.Fixed, summary.Failed, summary.Rounds)
	return summary, nil
}

func (a *Agent) runRound(ctx context.Context, tasks []worker.Task) []worker.Result {
	pool := worker.NewPoolWithRateLimit(a.cfg.Concurrency, a.cfg.RateLimit, a.executor)
	ch := pool.RunStream(ctx, tasks)
	results := make([]worker.Result, 0, len(tasks))
	for r := range ch {
		fmt.Printf("  completed: %s (success=%t)\n", r.File, r.Success)
		results = append(results, r)
	}
	return results
}

func (a *Agent) publishFixAttempt(ctx context.Context, r worker.Result, linterName string, round int, strategy loop.Strategy) {
	_ = a.pub.Publish(ctx, telemetry.Event{
		Timestamp: time.Now(),
		Type:      "fix_attempt",
		Data: map[string]any{
			"file":          r.File,
			"success":       r.Success,
			"duration":      r.Duration.String(),
			"issues":        r.IssuesFix,
			"error":         r.Error,
			"linter":        linterName,
			"round":         round + 1,
			"strategy":      strategy.String(),
			"provider":      r.Provider,
			"model":         r.Model,
			"prompt_tokens": r.PromptTokens,
			"output_tokens": r.OutputTokens,
		},
	})
}

func (a *Agent) publishRoundComplete(ctx context.Context, round int, linterName string, taskCount int, results []worker.Result) {
	fixed := 0
	failed := 0
	for _, r := range results {
		if r.Success {
			fixed += r.IssuesFix
		} else {
			failed++
		}
	}
	_ = a.pub.Publish(ctx, telemetry.Event{
		Timestamp: time.Now(),
		Type:      "round_complete",
		Data: map[string]any{
			"round":  round + 1,
			"linter": linterName,
			"tasks":  taskCount,
			"fixed":  fixed,
			"failed": failed,
		},
	})
}

func (a *Agent) runRaw(ctx context.Context, result linter.ParseResult, linterName string) (Summary, error) {
	fmt.Println("Could not parse structured issues; passing raw output to agent.")

	task := worker.Task{
		ID:        0,
		Dir:       a.cfg.TargetDir,
		RawOutput: result.RawOutput,
	}
	task.Prompt = worker.BuildRawPrompt(task)

	if a.cfg.DryRun {
		fmt.Println("Dry run - would send raw lint output to agent for analysis.")
		return Summary{TotalIssues: 1, Tasks: 1}, nil
	}

	pool := worker.NewPool(a.cfg.Concurrency, a.executor)
	results := pool.Run(ctx, []worker.Task{task})

	summary := Summary{TotalIssues: 1, Tasks: 1, Rounds: 1}
	for _, r := range results {
		if r.Success {
			summary.Fixed++
		} else {
			summary.Failed++
		}
		_ = a.pub.Publish(ctx, telemetry.Event{
			Timestamp: time.Now(),
			Type:      "fix_attempt",
			Data: map[string]any{
				"file":          "raw",
				"success":       r.Success,
				"duration":      r.Duration.String(),
				"issues":        1,
				"error":         r.Error,
				"linter":        linterName,
				"provider":      r.Provider,
				"model":         r.Model,
				"prompt_tokens": r.PromptTokens,
				"output_tokens": r.OutputTokens,
			},
		})
	}

	fmt.Printf("Results: %d fixed, %d failed out of %d tasks.\n", summary.Fixed, summary.Failed, summary.Tasks)
	return summary, nil
}

// buildPrompt selects the appropriate prompt builder based on provider kind and strategy.
func (a *Agent) buildPrompt(task worker.Task, strategy loop.Strategy, priorOutput string) string {
	if a.providerKind == provider.KindAPI {
		switch strategy {
		case loop.StrategyRetry:
			return worker.BuildAPIRetryPrompt(task, priorOutput)
		case loop.StrategyExploration:
			return worker.BuildAPIExplorationPrompt(task, priorOutput)
		default:
			return worker.BuildAPIPrompt(task)
		}
	}
	switch strategy {
	case loop.StrategyRetry:
		return worker.BuildRetryPrompt(task, priorOutput)
	case loop.StrategyExploration:
		return worker.BuildExplorationPrompt(task, priorOutput)
	default:
		return worker.BuildPrompt(task)
	}
}

func safeHistory(fh *loop.FileHistory) loop.FileHistory {
	if fh == nil {
		return loop.FileHistory{}
	}
	return *fh
}

// filterRetryableIssues removes issues for files that have exhausted all strategies.
// A file is removed if exploration was attempted and it's still stagnant.
func filterRetryableIssues(
	issues []linter.Issue,
	histories map[string]*loop.FileHistory,
	explorationAttempted map[string]bool,
	staleThreshold int,
) []linter.Issue {
	var retryable []linter.Issue
	for _, iss := range issues {
		if explorationAttempted[iss.File] && loop.DetectStagnation(safeHistory(histories[iss.File]), staleThreshold) {
			continue
		}
		retryable = append(retryable, iss)
	}
	return retryable
}
