# Sweeper Agent Skill

You are operating the **sweeper** tool, an AI-powered lint fixer that dispatches parallel Claude Code sub-agents to fix lint issues from any linter.

## Quick Start

```bash
cd /path/to/target/project
sweeper run                              # default: golangci-lint
sweeper run -- npm run lint              # arbitrary command
npm run lint | sweeper run               # piped stdin
```

## Commands

### `sweeper run`

Runs the full lint-fix-retry loop:

1. Executes a lint command (default: `golangci-lint run --out-format=line-number ./...`)
2. Parses output using multi-format detection (golangci-lint, generic `file:line:col`, minimal `file:line`, or raw fallback)
3. Groups structured issues by file into parallel fix tasks
4. Selects prompt strategy based on round number and file history (standard → retry → exploration)
5. Dispatches parallel Claude Code sub-agents (default: 3) to fix each file
6. Records outcomes to `.sweeper/telemetry/` with round and strategy metadata
7. Re-lints to check remaining issues; repeats with escalated prompts (if `--max-rounds > 1`)

**Input modes:**
- `sweeper run` - Default: runs golangci-lint
- `sweeper run -- <command>` - Run an arbitrary lint command (e.g., `npm run lint`, `cargo clippy`)
- `<command> | sweeper run` - Pipe existing lint output via stdin

**Flags:**
- `--target, -t <dir>` - Directory to lint and fix (default: `.`)
- `--concurrency, -c <n>` - Max parallel sub-agents (default: `3`)
- `--dry-run` - Show what would be fixed without running agents
- `--no-paper` - Disable the paper capture detect+warn
- `--max-rounds <n>` - Maximum retry rounds (default: `1` = single pass)
- `--stale-threshold <n>` - Consecutive non-improving rounds before exploration mode (default: `2`)
- `--vm` - Boot ephemeral stereOS VM, teardown on exit
- `--vm-name <name>` - Use existing VM by name (no managed lifecycle, implies `--vm`)
- `--vm-jcard <path>` - Custom jcard.toml path (implies `--vm`)

**Example runs:**
```bash
# Fix current directory with golangci-lint (default)
sweeper run

# Fix a specific project with 5 agents
sweeper run -t /path/to/project -c 5

# Use ESLint
sweeper run -- npx eslint --format unix .

# Use cargo clippy
sweeper run -- cargo clippy 2>&1

# Pipe existing lint output
cat lint-results.txt | sweeper run

# Preview fixes
sweeper run --dry-run
sweeper run --dry-run -- npm run lint

# Retry loop: re-lint after each round, escalate prompt strategy
sweeper run --max-rounds 3
sweeper run --max-rounds 5 --stale-threshold 3

# Run inside an ephemeral stereOS VM (full isolation)
sweeper run --vm -- npx eslint --quiet .

# VM with retry loop
sweeper run --vm --max-rounds 3 -c 5 -- npx eslint --quiet .

# Use an existing VM (skip boot/teardown)
sweeper run --vm-name my-vm -- npx eslint --quiet .

# Custom jcard for VM configuration
sweeper run --vm --vm-jcard ./custom-jcard.toml -- cargo clippy 2>&1
```

**Exit codes:**
- `0` - All tasks succeeded (or no issues found)
- `1` - One or more tasks failed

### `sweeper observe`

Analyzes past run telemetry and shows success rates per linter:

```bash
sweeper observe
sweeper observe --target /path/to/project
```

Output shows: linter name, attempt count, successes, success rate percentage, and token usage (aggregated from sweeper's own telemetry).

### `sweeper version`

Prints the current version.

## Prerequisites

Before running sweeper, ensure these are available:

1. **claude** - Claude Code CLI must be in PATH. The tool invokes `claude --print --dangerously-skip-permissions <prompt>` for each fix task.
2. **golangci-lint** (only for default mode) - Must be in PATH. Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
3. **paper** (optional) - When the `paper` CLI is installed (`paper init` to bring up the daemon), sweeper launches each claude sub-agent via `paper start claude`, so paper's gateway manages auth and captures the session (no `ANTHROPIC_API_KEY` used). Sweeper detects the `paper` CLI and warns if it's missing.
4. **mb** (optional, for `--vm` mode) - Masterblaster CLI for stereOS VMs. Required only when using `--vm` flag.

When using `-- <command>` or piped input, golangci-lint is not required.

## VM Isolation (stereOS)

Use `--vm` to run sub-agents inside a stereOS virtual machine. This provides:

- **Secret isolation**: `ANTHROPIC_API_KEY` and other credentials stay inside the VM. No risk of secrets bleeding into host processes, IDE plugins, or other tools sharing the same shell environment.
- **Resource isolation**: Sub-agents get dedicated CPU/memory inside the VM instead of competing with your IDE and other local processes.
- **No nesting conflicts**: `claude --print` fails inside active Claude Code sessions due to nesting detection. The VM sidesteps this entirely — agents run in a clean environment.
- **Clean teardown**: Ephemeral VMs are destroyed when sweeper exits (success, failure, or interrupt). Nothing persists.

### When to use `--vm`

| Scenario | Recommendation |
|---|---|
| Running sweeper inside a Claude Code session | Use `--vm` (avoids CLAUDECODE nesting error) |
| Working with sensitive API keys or tokens | Use `--vm` (prevents secret bleeding) |
| High concurrency (5+ agents) | Use `--vm` (dedicated resources) |
| Quick single-file fix | Skip `--vm` (local is faster) |
| CI/CD pipeline | Use `--vm` (hermetic environment) |

### Prerequisites for `--vm`

1. **mb** (Masterblaster CLI) must be in PATH. This is the stereOS VM manager.
2. **ANTHROPIC_API_KEY** must be set — it's passed into the VM via the jcard.

### Lifecycle

Sweeper manages the full VM lifecycle when `--vm` is used without a name:

1. Generates an ephemeral `jcard.toml` in `.sweeper/vm/`
2. Boots the VM via `mb up`
3. Executes all sub-agents inside the VM via SSH
4. Tears down the VM via `mb destroy` on exit (deferred — fires on success, failure, or SIGINT)

Use `--vm-name <name>` to attach to an existing VM. Sweeper skips boot and teardown — the VM is yours to manage.

## Building from Source

```bash
cd /path/to/sweeper
go build -o sweeper .
```

The binary has no CGO dependencies (uses pure-Go SQLite) and cross-compiles cleanly.

## How It Works

### Structured output (parsed)

When lint output matches a recognized format (`file:line:col: message`), each sub-agent receives a focused prompt for a single file:

```
Fix the following lint issues in path/to/file.go:

- Line 12: exported function Foo should have comment (golint)
- Line 45: unnecessary conversion (unconvert)

Fix each issue. Do not change behavior. Only fix lint issues. Commit nothing.
```

Multiple agents run concurrently across different files.

### Retry loop (RL-inspired)

When `--max-rounds > 1`, sweeper re-lints after each round and retries files with remaining issues. The prompt strategy escalates:

- **Round 1 (standard)**: Normal fix prompt with issue list
- **Round 2+ (retry)**: Includes prior attempt output, instructs agent to try a different approach
- **After stagnation (exploration)**: WARNING directive, instructs agent to refactor surrounding code

Stagnation is detected after `--stale-threshold` consecutive rounds with zero improvement on a file. After exploration is attempted and fails, the file is dropped from further retries.

**Internal loop architecture** (`pkg/loop/`):

- **Strategy enum**: `StrategyStandard`, `StrategyRetry`, `StrategyExploration` — selected by `PickStrategy(round, fileHistory, staleThreshold)`
- **FileHistory**: Tracks `RoundResult` per file across rounds (issues before/after, output, strategy used)
  - `Improved()` — did the latest round fix at least one issue?
  - `ConsecutiveStale()` — trailing count of rounds with zero improvement
  - `LastOutput()` — prior attempt output fed into retry/exploration prompts
- **DetectStagnation**: `ConsecutiveStale() >= staleThreshold` triggers exploration
- **filterRetryableIssues**: Drops files where exploration was attempted and stagnation persists

The agent loop (`pkg/agent/`) orchestrates: lint → group by file → pick strategy per file → build prompt → dispatch pool → publish telemetry → re-lint → update histories → filter retryable → repeat.

Telemetry events include `round` and `strategy` fields, enabling `sweeper observe` to show which rounds and strategies are most effective across runs.

**Historical insights** (`sweeper observe` with multi-round data):
- **Success rate trend**: Per-run success rates over time
- **Round effectiveness**: Fraction of total fixes contributed by each round number
- **Strategy effectiveness**: Success rate per prompt strategy (standard/retry/exploration)

### Raw output (fallback)

When output cannot be parsed into structured issues, the full output is sent to a single agent for analysis:

```
The following lint output was produced. Analyze it, identify the issues, and fix them:

<full lint output>

Fix each issue you can identify. Do not change behavior. Only fix lint issues. Commit nothing.
```

## Telemetry

Results are stored in `.sweeper/telemetry/YYYY-MM-DD.jsonl` relative to the target directory.

Event types:
- **fix_attempt**: Per-file fix result with file, success, duration, issue count, linter, round number, and prompt strategy
- **round_complete**: Per-round summary with task count, fixed count, and failed count

Use `sweeper observe` to analyze this data. It shows success rates per linter and, when multi-round telemetry exists, round effectiveness and strategy effectiveness trends.

## Troubleshooting

- **"golangci-lint: command not found"** - Install golangci-lint or use `-- <command>` to specify a different linter
- **"claude: command not found"** - Install Claude Code CLI or add it to PATH
- **"cannot use both piped input and -- command"** - Choose one input method: pipe or `--`
- **"No lint issues found"** - The target codebase is clean; nothing to fix
- **Custom command produces no parseable output** - Sweeper falls back to raw mode; the agent will analyze the full output
- **Paper warning** (`paper CLI not found`) - Install paper and run `paper init` to capture sessions, or use `--no-paper` to suppress the warning
- **Tasks failing** - Check the sub-agent output in the telemetry JSONL for error details
