# VM Ladder Per-Rung Models Implementation Plan (issue #28)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the escalation ladder and the advisor work under `--vm` by plumbing model selection into the VM executor and constructing per-rung VM executors, removing the warn-and-disable gates.

**Architecture:** `worker.NewVMExecutor` gains a config struct carrying the claude model. `agent.New` is restructured to apply options *before* cfg resolution and gains a `WithVMExecutorFactory` option — a `func(model string) worker.Executor` closure that `cmd/run.go` builds over the VM handle. Under `cfg.VM`, the base worker, each ladder rung, and the advisor are constructed through that factory (claude models only; cross-provider rungs stay unsupported and disable the ladder with a warning). `vm.Exec` also starts honoring its context so the advisor's 5-minute timeout can actually cancel a hung VM invocation.

**Tech Stack:** Go 1.x, stdlib `testing`, cobra CLI. No new dependencies.

## Global Constraints

- Repo: `github.com/papercomputeco/sweeper`, run everything from the worktree root.
- All warnings keep the existing style: `fmt.Printf("Warning: ...\n")`, and a bad ladder entry disables the *whole* ladder (all-or-nothing, matching current behavior).
- Cross-provider rungs inside VMs are **out of scope** (issue #28): under `--vm`, every rung and the advisor must resolve to provider `"claude"`.
- Existing option semantics must survive: values injected via `With*` options win over cfg-derived resolution (tests rely on this).
- Test commands: `go test ./pkg/<pkg>/ -run <Name> -v`; full gate is `go build ./... && go vet ./... && go test ./...`.
- Commit style (from git log): `✨ feat: ...`, `fix: ...`, `docs: ...`, reference `(#28)` in the subject of feature commits.

---

### Task 1: Plumb model selection into the VM executor

**Files:**
- Modify: `pkg/worker/vm.go`
- Modify: `pkg/worker/vm_test.go`
- Modify: `cmd/run.go:164,177` (two `NewVMExecutor` call sites — keep build green, fixes #20's base-model gap)

**Interfaces:**
- Consumes: `worker.VMExecer` (unchanged), `worker.Result` fields `Provider`/`Model` (exist already, see `pkg/worker/result.go`).
- Produces: `worker.VMExecConfig{Model string}` and `worker.NewVMExecutor(vm VMExecer, cfg VMExecConfig) Executor`. Results now carry `Provider: "claude"` and `Model: cfg.Model` on both success and failure paths. Task 3's factory and Task 4's wiring call this exact signature.

- [ ] **Step 1: Write the failing tests**

In `pkg/worker/vm_test.go`, update the two existing tests to the new signature and add two model-flag tests. Full new file content for the changed parts:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestNewVMExecutor -v`
Expected: compile FAIL — `too many arguments in call to NewVMExecutor` / `undefined: VMExecConfig`.

- [ ] **Step 3: Implement**

Replace the body of `pkg/worker/vm.go` (keep the `VMExecer` interface as is):

```go
// VMExecConfig holds settings for the VM executor.
type VMExecConfig struct {
	Model string // e.g. "claude-haiku-4-5"; empty uses the CLI's default
}

// NewVMExecutor returns an Executor that runs claude inside a stereOS VM.
func NewVMExecutor(vm VMExecer, cfg VMExecConfig) Executor {
	return func(ctx context.Context, task Task) Result {
		start := time.Now()
		args := []string{"claude", "--print", "--dangerously-skip-permissions"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, task.Prompt)
		out, err := vm.Exec(ctx, args...)
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
```

Update both call sites in `cmd/run.go` (lines 164 and 177) so the base VM worker honors the configured model:

```go
opts = append(opts, agent.WithExecutor(worker.NewVMExecutor(vmHandle, worker.VMExecConfig{Model: cfg.ProviderModel})))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./pkg/worker/ -run TestNewVMExecutor -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/vm.go pkg/worker/vm_test.go cmd/run.go
git commit -m "✨ feat: plumb model selection into the VM executor (#28)"
```

---

### Task 2: Propagate context through VM command execution

**Files:**
- Modify: `pkg/vm/vm.go`
- Modify: `pkg/vm/vm_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: unchanged public API (`Boot`, `Attach`, `(*VM).Exec(ctx, args...)`, `Shutdown`), but `Exec` now actually cancels the `mb ssh` process when ctx is done. Internal `cmdRunner` becomes `func(ctx context.Context, name string, args ...string) ([]byte, error)`. This is what makes the advisor's 5-minute timeout (`pkg/advisor/advisor.go:15`) effective under `--vm`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/vm/vm_test.go`:

```go
func TestVMExecPropagatesContext(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	var gotCtx context.Context
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotCtx = ctx
		return nil, nil
	}
	vm := &VM{Name: "t", runner: runner}
	if _, err := vm.Exec(ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	if gotCtx == nil || gotCtx.Value(ctxKey{}) != "marker" {
		t.Error("Exec should pass its context through to the runner")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/vm/ -run TestVMExecPropagatesContext -v`
Expected: compile FAIL — the fake runner's signature doesn't match `cmdRunner`.

- [ ] **Step 3: Implement**

In `pkg/vm/vm.go`:

```go
type cmdRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
```

Update the three runner invocations:

```go
// in boot():
out, err := runner(context.Background(), "mb", "up", "--config", jcardPath)

// in Exec():
return v.runner(ctx, "mb", mbArgs...)

// in Shutdown():
out, err := v.runner(context.Background(), "mb", "destroy", v.Name, "--yes")
```

Update every fake runner in `pkg/vm/vm_test.go` (in `TestVMBootCallsMbUp`, `TestVMBootError`, `TestVMBootJcardError`, `TestVMShutdownManaged`, `TestVMShutdownUnmanaged`, `TestVMExec`, `TestVMExecError`, `TestVMShutdownError`) to the new signature by adding the leading parameter, e.g.:

```go
runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
	gotArgs = append([]string{name}, args...)
	return []byte("ok"), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/vm/ -v`
Expected: PASS (all vm package tests, including the new one).

- [ ] **Step 5: Commit**

```bash
git add pkg/vm/vm.go pkg/vm/vm_test.go
git commit -m "fix: propagate context through VM command execution"
```

---

### Task 3: Agent constructs ladder and advisor under --vm via an executor factory

**Files:**
- Modify: `pkg/agent/agent.go` (the `Agent` struct, options, and `New`)
- Modify: `pkg/agent/agent_test.go` (two renamed tests, five new tests)

**Interfaces:**
- Consumes: `worker.Executor`, `provider.ParseRung(entry, defaultProvider)`, `provider.Get(name)`, `provider.KindCLI`.
- Produces: `agent.WithVMExecutorFactory(f func(model string) worker.Executor) Option`. Under `cfg.VM` with a factory present: base executor = `factory(cfg.ProviderModel)`, each ladder rung = `factory(rungModel)` (rung provider must be `"claude"`), advisor = `factory(cfg.AdvisorModel)` (advisor provider must be `"claude"`). Task 4 calls this option.
- Invariant preserved: options injected via `WithExecutor`/`WithLadder`/`WithAdvisorExecutor` still win — `New` now applies options *first* and cfg resolution only fills fields that are still nil.

- [ ] **Step 1: Write the failing tests**

In `pkg/agent/agent_test.go`, rename the two gate tests and pin the new no-factory semantics (VM mode without a factory still disables both — defensive path, same observable behavior as today):

```go
func TestNewAgentAdvisorDisabledInVMModeWithoutFactory(t *testing.T) {
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
		t.Error("expected advisor disabled in VM mode when no VM executor factory is wired")
	}
}

func TestNewAgentLadderDisabledInVMModeWithoutFactory(t *testing.T) {
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
		t.Error("expected ladder disabled in VM mode when no VM executor factory is wired")
	}
}
```

Add the five new tests (place them after `TestNewAgentLadderDisabledInVMModeWithoutFactory`). The shared factory records which models it was asked for:

```go
// vmFactoryRecorder returns a VM executor factory that records the models
// it is asked to build executors for.
func vmFactoryRecorder(models *[]string) func(model string) worker.Executor {
	return func(model string) worker.Executor {
		*models = append(*models, model)
		return func(ctx context.Context, task worker.Task) worker.Result {
			return worker.Result{TaskID: task.ID, File: task.File, Success: true}
		}
	}
}

func TestNewAgentVMBaseExecutorFromFactory(t *testing.T) {
	var models []string
	cfg := config.Config{
		TargetDir:     t.TempDir(),
		Concurrency:   1,
		TelemetryDir:  t.TempDir(),
		Provider:      "claude",
		ProviderModel: "claude-haiku-4-5",
		VM:            true,
	}
	a := New(cfg, WithVMExecutorFactory(vmFactoryRecorder(&models)))
	if a.executor == nil {
		t.Fatal("expected base executor constructed from VM factory")
	}
	if len(models) != 1 || models[0] != "claude-haiku-4-5" {
		t.Errorf("expected factory called once with base model, got %v", models)
	}
	if a.providerKind != provider.KindCLI {
		t.Errorf("expected KindCLI under VM, got %v", a.providerKind)
	}
}

func TestNewAgentVMLadderBuiltViaFactory(t *testing.T) {
	var models []string
	cfg := config.Config{
		TargetDir:        t.TempDir(),
		Concurrency:      1,
		TelemetryDir:     t.TempDir(),
		Provider:         "claude",
		ProviderModel:    "claude-haiku-4-5",
		EscalationLadder: []string{"claude-sonnet-5", "claude/claude-opus-4-8"},
		VM:               true,
	}
	a := New(cfg, WithVMExecutorFactory(vmFactoryRecorder(&models)))
	if len(a.ladder) != 2 {
		t.Fatalf("expected 2 rungs under --vm, got %d", len(a.ladder))
	}
	want := []string{"claude-haiku-4-5", "claude-sonnet-5", "claude-opus-4-8"}
	if len(models) != len(want) {
		t.Fatalf("expected factory models %v, got %v", want, models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("factory model[%d]: expected %q, got %q", i, want[i], models[i])
		}
	}
	if a.ladder[0].Provider != "claude" || a.ladder[0].Model != "claude-sonnet-5" {
		t.Errorf("unexpected rung 1: %+v", a.ladder[0])
	}
	if a.ladder[1].Provider != "claude" || a.ladder[1].Model != "claude-opus-4-8" {
		t.Errorf("unexpected rung 2: %+v", a.ladder[1])
	}
	if a.ladder[0].Kind != provider.KindCLI || a.ladder[1].Kind != provider.KindCLI {
		t.Error("expected KindCLI rungs under --vm")
	}
	if a.ladder[0].Exec == nil || a.ladder[1].Exec == nil {
		t.Error("expected rung executors constructed")
	}
}

func TestNewAgentVMLadderCrossProviderRungDisabled(t *testing.T) {
	var models []string
	cfg := config.Config{
		TargetDir:        t.TempDir(),
		Concurrency:      1,
		TelemetryDir:     t.TempDir(),
		Provider:         "claude",
		EscalationLadder: []string{"claude-sonnet-5", "ollama/qwen2.5-coder:32b"},
		VM:               true,
	}
	a := New(cfg, WithVMExecutorFactory(vmFactoryRecorder(&models)))
	if a.ladder != nil {
		t.Error("expected ladder disabled when a rung resolves to a non-claude provider under --vm")
	}
}

func TestNewAgentVMAdvisorBuiltViaFactory(t *testing.T) {
	var models []string
	cfg := config.Config{
		TargetDir:    t.TempDir(),
		Concurrency:  1,
		TelemetryDir: t.TempDir(),
		Provider:     "claude",
		AdvisorModel: "claude-opus-4-8",
		VM:           true,
	}
	a := New(cfg, WithVMExecutorFactory(vmFactoryRecorder(&models)))
	if a.advisorExec == nil {
		t.Fatal("expected advisor enabled under --vm with a VM executor factory")
	}
	if a.advisorProvider != "claude" || a.advisorModel != "claude-opus-4-8" {
		t.Errorf("unexpected advisor metadata: %q/%q", a.advisorProvider, a.advisorModel)
	}
	found := false
	for _, m := range models {
		if m == "claude-opus-4-8" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected factory called with advisor model, got %v", models)
	}
}

func TestNewAgentVMAdvisorNonClaudeProviderDisabled(t *testing.T) {
	var models []string
	cfg := config.Config{
		TargetDir:       t.TempDir(),
		Concurrency:     1,
		TelemetryDir:    t.TempDir(),
		Provider:        "claude",
		AdvisorProvider: "codex",
		AdvisorModel:    "o4-mini",
		VM:              true,
	}
	a := New(cfg, WithVMExecutorFactory(vmFactoryRecorder(&models)))
	if a.advisorExec != nil {
		t.Error("expected advisor disabled for non-claude advisor provider under --vm")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/agent/ -run 'TestNewAgentVM|TestNewAgentAdvisorDisabledInVMModeWithoutFactory|TestNewAgentLadderDisabledInVMModeWithoutFactory' -v`
Expected: compile FAIL — `undefined: WithVMExecutorFactory` (and the old test names no longer exist).

- [ ] **Step 3: Implement**

In `pkg/agent/agent.go`:

Add the field to `Agent` (after `ladder []LadderRung`):

```go
	vmExecFactory func(model string) worker.Executor
```

Add the option (after `WithVM`):

```go
// WithVMExecutorFactory supplies the constructor for executors that run
// inside the sweep VM. Under cfg.VM the base worker, escalation rungs, and
// advisor are built through this factory (claude models only) instead of
// the provider registry.
func WithVMExecutorFactory(f func(model string) worker.Executor) Option {
	return func(a *Agent) { a.vmExecFactory = f }
}
```

Replace `New` with:

```go
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
```

Note the trailing `for _, opt := range opts` loop from the old `New` is gone — it moved to the top.

- [ ] **Step 4: Run the full agent package tests**

Run: `go test ./pkg/agent/ -v 2>&1 | tail -30`
Expected: PASS — all existing tests (options-first must not regress `WithExecutor`/`WithLadder`/`WithAdvisorExecutor` injection) plus the 7 from Step 1.

- [ ] **Step 5: Commit**

```bash
git add pkg/agent/agent.go pkg/agent/agent_test.go
git commit -m "✨ feat: build ladder and advisor under --vm via per-rung VM executors (#28)"
```

---

### Task 4: Wire the VM executor factory through `sweeper run`

**Files:**
- Modify: `cmd/run.go:159-180` (the `if useVM` block)

**Interfaces:**
- Consumes: `agent.WithVM`, `agent.WithVMExecutorFactory` (Task 3), `worker.NewVMExecutor(vmHandle, worker.VMExecConfig{Model: model})` (Task 1), `vm.Attach`/`vm.Boot` (unchanged).
- Produces: under `--vm`, `agent.New` receives the factory and builds the base executor itself, so the explicit `agent.WithExecutor(...)` injection is removed.

- [ ] **Step 1: Replace the `if useVM` block**

Replace lines 159–180 of `cmd/run.go` with:

```go
			if useVM {
				absTarget, _ := filepath.Abs(cfg.TargetDir)
				var vmHandle *vm.VM
				if cfg.VMName != "" {
					vmHandle = vm.Attach(cfg.VMName, absTarget)
					fmt.Printf("VM: using existing VM %s\n", cfg.VMName)
				} else {
					name := vm.NewVMName()
					jcardDir := filepath.Join(absTarget, ".sweeper", "vm")
					if cfg.VMJcard != "" {
						jcardDir = filepath.Dir(cfg.VMJcard)
					}
					booted, err := vm.Boot(name, absTarget, jcardDir)
					if err != nil {
						return fmt.Errorf("booting VM: %w", err)
					}
					vmHandle = booted
					fmt.Printf("VM: booted %s (managed, will teardown on exit)\n", name)
				}
				opts = append(opts, agent.WithVM(vmHandle))
				opts = append(opts, agent.WithVMExecutorFactory(func(model string) worker.Executor {
					return worker.NewVMExecutor(vmHandle, worker.VMExecConfig{Model: model})
				}))
			}
```

(The `worker` import is already present from Task 1; `vm` was already imported.)

- [ ] **Step 2: Build and run the full suite**

Run: `go build ./... && go test ./cmd/ ./pkg/... 2>&1 | tail -20`
Expected: build OK, all packages PASS (cmd has no test files — `[no test files]` is expected).

- [ ] **Step 3: Smoke-check the flag surface**

Run: `go run . run --help | grep -E 'vm|model'`
Expected: `--vm`, `--vm-name`, `--vm-jcard`, `--model`, `--advisor-model` all still listed.

- [ ] **Step 4: Commit**

```bash
git add cmd/run.go
git commit -m "✨ feat: wire VM executor factory through sweeper run (#28)"
```

---

### Task 5: Update docs — README, example.config.toml, skill docs

**Files:**
- Modify: `README.md` (Advisor Phase section ~line 62, Model Escalation section ~line 68, provider-compat line ~165)
- Modify: `example.config.toml` (`[worker.escalation]` and `[advisor]` comment blocks)
- Modify: `skills/sweeper/SKILL.md:95` (provider/VM compat sentence)

**Interfaces:** documentation only; must describe exactly the behavior shipped in Tasks 1–4.

- [ ] **Step 1: README — Model Escalation section**

At the end of the "Model Escalation" section (after the paragraph ending "...can pin a gnarly file to a stronger starting rung via its `tier` hint."), add:

```markdown
Both work under `--vm`: rungs and the advisor dispatch `claude --model <model>` inside the VM. Because VM workers always run claude, every rung (and the advisor provider) must resolve to claude when VM isolation is on — a cross-provider rung like `ollama/...` disables the ladder with a warning.
```

- [ ] **Step 2: README — provider compatibility line**

Change line ~165 from:

```markdown
VM isolation (`--vm`) is only compatible with CLI providers.
```

to:

```markdown
VM isolation (`--vm`) is only compatible with CLI providers, and currently always invokes claude inside the VM (the worker `--model`, escalation rungs, and advisor model are honored).
```

- [ ] **Step 3: example.config.toml**

In the `[worker.escalation]` comment block, after the line `# endpoint; api_base above only applies to the worker's own provider.`, add:

```toml
# With [vm] enabled, rungs run `claude --model <rung>` inside the VM, so
# every entry must resolve to the claude provider.
```

In the `[advisor]` comment block, after the line `# Requires a CLI provider (claude, codex). Unset = disabled (mechanical plan).`, add:

```toml
# With [vm] enabled, the advisor runs inside the VM and must use claude.
```

- [ ] **Step 4: skills/sweeper/SKILL.md**

Change line 95's final sentence from:

```markdown
VM isolation (`--vm`) only works with CLI providers.
```

to:

```markdown
VM isolation (`--vm`) only works with CLI providers and always invokes claude inside the VM; worker/rung/advisor models are passed via `--model`.
```

- [ ] **Step 5: Full verification gate**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build OK, vet clean, all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add README.md example.config.toml skills/sweeper/SKILL.md
git commit -m "docs: ladder and advisor now work under --vm (claude models only) (#28)"
```
