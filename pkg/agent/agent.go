package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/papercomputeco/sweeper/pkg/advisor"
	"github.com/papercomputeco/sweeper/pkg/config"
	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/planner"
	"github.com/papercomputeco/sweeper/pkg/provider"
	"github.com/papercomputeco/sweeper/pkg/session"
	"github.com/papercomputeco/sweeper/pkg/telemetry"
	"github.com/papercomputeco/sweeper/pkg/worker"
)

type LinterFunc func(ctx context.Context, dir string) (linter.ParseResult, error)

// LadderRung is one escalation step above the base worker: a pooled
// executor plus the metadata needed to route prompts and telemetry.
type LadderRung struct {
	Exec     worker.Executor
	Kind     provider.Kind
	Provider string
	Model    string
}

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

	advisorExec     worker.Executor
	advisorProvider string
	advisorModel    string
	ladder          []LadderRung
	vmExecFactory   func(model string) worker.Executor
}

type Option func(*Agent)

func WithLinterFunc(fn LinterFunc) Option {
	return func(a *Agent) { a.linterFn = fn }
}

func WithExecutor(exec worker.Executor) Option {
	return func(a *Agent) { a.executor = exec }
}

// WithAdvisorExecutor injects the executor used for the sweep-planning
// advisor phase. Primarily for tests; production wiring resolves it from
// the provider registry via cfg.AdvisorProvider/AdvisorModel.
func WithAdvisorExecutor(exec worker.Executor) Option {
	return func(a *Agent) { a.advisorExec = exec }
}

// WithLadder injects escalation rungs 1..N above the base executor.
// Primarily for tests; production wiring parses cfg.EscalationLadder.
func WithLadder(rungs []LadderRung) Option {
	return func(a *Agent) { a.ladder = rungs }
}

func WithVM(vm VMManager) Option {
	return func(a *Agent) { a.vm = vm }
}

// WithVMExecutorFactory supplies the constructor for executors that run
// inside the sweep VM. Under cfg.VM the base worker, escalation rungs, and
// advisor are built through this factory (claude models only) instead of
// the provider registry.
func WithVMExecutorFactory(f func(model string) worker.Executor) Option {
	return func(a *Agent) { a.vmExecFactory = f }
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

	// Options apply before cfg resolution so injected executors take
	// precedence and resolution only fills what is still unset.
	for _, opt := range opts {
		opt(a)
	}

	// Resolve provider from registry; fall back to Claude if lookup fails
	// (cmd/run.go validates before reaching here, so fallback is defensive).
	provName := cfg.Provider
	if provName == "" {
		provName = "claude"
	}
	p, perr := provider.Get(provName)
	if perr == nil {
		a.providerKind = p.Kind
	} else {
		fmt.Printf("Warning: unknown provider %q, falling back to claude\n", provName)
		a.providerKind = provider.KindCLI
	}
	if a.executor == nil {
		switch {
		case cfg.VM && a.vmExecFactory != nil:
			// VM executors always run the claude CLI inside the VM.
			a.executor = a.vmExecFactory(cfg.ProviderModel)
			a.providerKind = provider.KindCLI
		case perr == nil:
			a.executor = p.NewExec(provider.Config{
				Model:   cfg.ProviderModel,
				APIBase: cfg.ProviderAPI,
			})
		default:
			a.executor = worker.NewClaudeExecutor(worker.ClaudeConfig{Model: cfg.ProviderModel})
		}
	}

	// Resolve the optional sweep-planning advisor. The advisor is a one-shot
	// planning call, so it requires a CLI provider whose output is plain text
	// (KindAPI executors apply diffs and cannot answer planning prompts).
	// Under --vm it runs inside the VM, which only supports claude.
	if (cfg.AdvisorProvider != "" || cfg.AdvisorModel != "") && a.advisorExec == nil {
		advName := cfg.AdvisorProvider
		if advName == "" {
			advName = "claude"
		}
		switch {
		case cfg.VM && a.vmExecFactory == nil:
			fmt.Printf("Warning: advisor requires a VM executor with --vm; advisor disabled\n")
		case cfg.VM && advName != "claude":
			fmt.Printf("Warning: advisor provider %q is not supported with --vm (only claude); advisor disabled\n", advName)
		case cfg.VM:
			a.advisorExec = a.vmExecFactory(cfg.AdvisorModel)
			a.advisorProvider = advName
			a.advisorModel = cfg.AdvisorModel
		default:
			if p, err := provider.Get(advName); err != nil {
				fmt.Printf("Warning: unknown advisor provider %q, advisor disabled\n", advName)
			} else if p.Kind != provider.KindCLI {
				fmt.Printf("Warning: advisor requires a CLI provider, got %q; advisor disabled\n", advName)
			} else {
				a.advisorExec = p.NewExec(provider.Config{Model: cfg.AdvisorModel})
				a.advisorProvider = advName
				a.advisorModel = cfg.AdvisorModel
			}
		}
	}

	// Resolve the escalation ladder: rungs above the base worker, climbed
	// per file on stagnation. Executors are constructed once and reused.
	// Under --vm rungs run inside the VM, which only supports claude models;
	// a rung on any other provider disables the ladder.
	if len(cfg.EscalationLadder) > 0 && a.ladder == nil {
		if cfg.VM && a.vmExecFactory == nil {
			fmt.Printf("Warning: escalation ladder requires a VM executor with --vm; ladder disabled\n")
		} else {
			rungs := make([]LadderRung, 0, len(cfg.EscalationLadder))
			for _, entry := range cfg.EscalationLadder {
				entry = strings.TrimSpace(entry)
				if entry == "" {
					fmt.Printf("Warning: empty escalation ladder entry; ladder disabled\n")
					rungs = nil
					break
				}
				rungProv, rungModel := provider.ParseRung(entry, provName)
				if cfg.VM {
					if rungProv != "claude" {
						fmt.Printf("Warning: escalation rung %q: provider %q is not supported with --vm (only claude); ladder disabled\n", entry, rungProv)
						rungs = nil
						break
					}
					rungs = append(rungs, LadderRung{
						Exec:     a.vmExecFactory(rungModel),
						Kind:     provider.KindCLI,
						Provider: rungProv,
						Model:    rungModel,
					})
					continue
				}
				p, err := provider.Get(rungProv)
				if err != nil {
					fmt.Printf("Warning: escalation rung %q: %v; ladder disabled\n", entry, err)
					rungs = nil
					break
				}
				apiBase := ""
				if rungProv == provName {
					apiBase = cfg.ProviderAPI
				}
				rungs = append(rungs, LadderRung{
					Exec:     p.NewExec(provider.Config{Model: rungModel, APIBase: apiBase}),
					Kind:     p.Kind,
					Provider: rungProv,
					Model:    rungModel,
				})
			}
			a.ladder = rungs
		}
	}

	return a
}

func (a *Agent) Run(ctx context.Context) (Summary, error) {
	defer func() { _ = a.pub.Close() }()
	if a.vm != nil {
		defer func() { _ = a.vm.Shutdown() }()
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
	// explorationRungs records, per file, the HIGHEST rung at which
	// exploration has been dispatched. Presence (not the value) distinguishes
	// "never explored" from "explored at rung 0" — check with the
	// comma-ok form, not a zero-value sentinel.
	explorationRungs := make(map[string]int)

	var esc *loop.Escalation
	if len(a.ladder) > 0 {
		esc = loop.NewEscalation(len(a.ladder))
	}

	for round := 0; round < maxRounds; round++ {
		if len(issues) == 0 {
			break
		}

		fixTasks := planner.GroupByFile(issues)
		var hints map[string]advisor.PlannedTask
		if a.advisorExec != nil && !a.cfg.DryRun {
			fixTasks, hints = a.advise(ctx, round, fixTasks, fileHistories)
			if esc != nil {
				for file, hint := range hints {
					if idx := a.rungIndexForModel(hint.Tier); idx > 0 {
						esc.Seed(file, idx)
					}
				}
			}
		}
		tasks := make([]worker.Task, len(fixTasks))
		strategies := make([]loop.Strategy, len(fixTasks))
		rungs := make([]int, len(fixTasks))

		for i, ft := range fixTasks {
			fh := safeHistory(fileHistories[ft.File])
			strategy := loop.PickStrategy(round, fh, a.cfg.StaleThreshold)
			if hint, ok := hints[ft.File]; ok {
				strategy = advisor.ResolveStrategy(hint.Strategy, round, fh, a.cfg.StaleThreshold)
			}
			strategies[i] = strategy

			rung := 0
			if esc != nil {
				// Only a stagnant file may climb the ladder: an advisor
				// exploration hint alone (ResolveStrategy honors it whenever
				// history exists) must not burn a rung on a file that is
				// still improving. The exploration prompt is still honored
				// either way — only the ladder climb is gated.
				if strategy == loop.StrategyExploration && loop.DetectStagnation(fh, a.cfg.StaleThreshold) {
					rung = esc.Bump(ft.File)
					if a.ladder[rung-1].Model != "" {
						fmt.Printf("  escalating %s to %s\n", ft.File, a.ladder[rung-1].Model)
					}
				} else {
					rung = esc.Rung(ft.File)
				}
			}
			rungs[i] = rung

			tasks[i] = worker.Task{
				ID:     i,
				File:   ft.File,
				Dir:    a.cfg.TargetDir,
				Issues: ft.Issues,
			}
			tasks[i].Prompt = a.buildPromptForKind(a.rungKind(rung), tasks[i], strategy, fh.LastOutput())
			if strategy == loop.StrategyExploration {
				if prev, ok := explorationRungs[ft.File]; !ok || rung > prev {
					explorationRungs[ft.File] = rung
				}
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

		exec := a.executor
		if esc != nil {
			execByID := make(map[int]worker.Executor, len(tasks))
			for i := range tasks {
				execByID[tasks[i].ID] = a.rungExecutor(rungs[i])
			}
			exec = func(ctx context.Context, task worker.Task) worker.Result {
				return execByID[task.ID](ctx, task)
			}
		}
		results := a.runRound(ctx, tasks, exec)
		summary.Rounds = round + 1

		for i, r := range results {
			strategy := strategies[i]
			a.publishFixAttempt(ctx, r, linterName, round, strategy, rungs[r.TaskID])

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
		issues = filterRetryableIssues(reResult.Issues, fileHistories, explorationRungs, a.cfg.StaleThreshold, esc)

		if a.sessionPath != "" {
			_ = session.UpdateStatus(a.sessionPath, round+1, len(reResult.Issues), summary.Fixed, len(issues))
		}
	}

	fmt.Printf("Results: %d fixed, %d failed (%d rounds).\n", summary.Fixed, summary.Failed, summary.Rounds)
	return summary, nil
}

func (a *Agent) runRound(ctx context.Context, tasks []worker.Task, exec worker.Executor) []worker.Result {
	pool := worker.NewPoolWithRateLimit(a.cfg.Concurrency, a.cfg.RateLimit, exec)
	ch := pool.RunStream(ctx, tasks)
	results := make([]worker.Result, 0, len(tasks))
	for r := range ch {
		fmt.Printf("  completed: %s (success=%t)\n", r.File, r.Success)
		results = append(results, r)
	}
	return results
}

// rungExecutor returns the executor for a rung; rung 0 is the base worker.
func (a *Agent) rungExecutor(rung int) worker.Executor {
	if rung <= 0 || rung > len(a.ladder) {
		return a.executor
	}
	return a.ladder[rung-1].Exec
}

// rungIndexForModel maps an advisor tier hint to a ladder rung index
// (1-based; 0 means no match). Hints match a rung's model name exactly or
// as "provider/model".
func (a *Agent) rungIndexForModel(tier string) int {
	if tier == "" {
		return 0
	}
	for i, r := range a.ladder {
		if tier == r.Model || tier == r.Provider+"/"+r.Model {
			return i + 1
		}
	}
	return 0
}

// rungKind returns the provider kind for a rung; rung 0 is the base worker.
func (a *Agent) rungKind(rung int) provider.Kind {
	if rung <= 0 || rung > len(a.ladder) {
		return a.providerKind
	}
	return a.ladder[rung-1].Kind
}

// advise runs the advisor phase for a round: build the planning prompt, run
// the advisor executor, and overlay the plan on the mechanical grouping.
// Every outcome is published as an advisor_plan event; on any failure the
// mechanical plan is returned unchanged.
func (a *Agent) advise(ctx context.Context, round int, tasks []planner.FixTask, histories map[string]*loop.FileHistory) ([]planner.FixTask, map[string]advisor.PlannedTask) {
	start := time.Now()
	hist := make(map[string]loop.FileHistory, len(histories))
	for f, fh := range histories {
		hist[f] = safeHistory(fh)
	}

	data := map[string]any{
		"round":       round + 1,
		"provider":    a.advisorProvider,
		"model":       a.advisorModel,
		"files_input": len(tasks),
	}
	tiers := make([]string, 0, len(a.ladder))
	for _, r := range a.ladder {
		tiers = append(tiers, r.Model)
	}
	plan, err := advisor.Advise(ctx, a.advisorExec, a.cfg.TargetDir, tasks, hist, round, tiers)
	data["duration"] = time.Since(start).String()
	if err != nil {
		fmt.Printf("Warning: advisor failed (%v); using mechanical plan\n", err)
		data["success"] = false
		data["error"] = err.Error()
		_ = a.pub.Publish(ctx, telemetry.Event{Timestamp: time.Now(), Type: "advisor_plan", Data: data})
		return tasks, nil
	}

	ordered, hints := advisor.Apply(plan, tasks)
	data["success"] = true
	data["files_planned"] = len(plan.Tasks)
	_ = a.pub.Publish(ctx, telemetry.Event{Timestamp: time.Now(), Type: "advisor_plan", Data: data})
	fmt.Printf("Advisor planned %d tasks.\n", len(ordered))
	return ordered, hints
}

func (a *Agent) publishFixAttempt(ctx context.Context, r worker.Result, linterName string, round int, strategy loop.Strategy, rung int) {
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
			"rung":          rung,
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

// buildPromptForKind selects the appropriate prompt builder for the
// dispatching rung's provider kind and strategy.
func (a *Agent) buildPromptForKind(kind provider.Kind, task worker.Task, strategy loop.Strategy, priorOutput string) string {
	if kind == provider.KindAPI {
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

// buildPrompt selects the prompt for the base worker's provider kind.
func (a *Agent) buildPrompt(task worker.Task, strategy loop.Strategy, priorOutput string) string {
	return a.buildPromptForKind(a.providerKind, task, strategy, priorOutput)
}

func safeHistory(fh *loop.FileHistory) loop.FileHistory {
	if fh == nil {
		return loop.FileHistory{}
	}
	return *fh
}

// filterRetryableIssues removes issues for files that have exhausted all
// strategies. Without a ladder, a file is removed once exploration was
// attempted and it is still stagnant. With a ladder, the file survives
// until exploration has been attempted at the TOP rung — not merely until
// the file's current rung is the top rung (a seed or bump can reach the top
// rung without exploration ever having been dispatched there).
func filterRetryableIssues(
	issues []linter.Issue,
	histories map[string]*loop.FileHistory,
	explorationRungs map[string]int,
	staleThreshold int,
	esc *loop.Escalation,
) []linter.Issue {
	var retryable []linter.Issue
	for _, iss := range issues {
		rung, explored := explorationRungs[iss.File]
		stagnant := explored && loop.DetectStagnation(safeHistory(histories[iss.File]), staleThreshold)
		if stagnant && (esc == nil || rung >= esc.Top()) {
			continue
		}
		retryable = append(retryable, iss)
	}
	return retryable
}
