---
name: sweeper
description: Agent-powered code maintenance with parallel sub-agents, VM isolation, and telemetry-driven learning. Orchestrates sweeper CLI to dispatch concurrent agents for lint fixes, test repairs, migrations, refactoring, and any measurable code improvement target.
---

# Sweeper - Agent-Powered Code Maintenance

You orchestrate the **sweeper** CLI to run parallel AI sub-agents against a codebase with optional VM isolation and swappable providers (Claude, Codex, Ollama/local models). While lint fixing is the default, the same loop handles test repairs, dependency migrations, refactoring, and any task where you can run a command, parse issues, and dispatch agents to fix them. Sweeper's JSONL telemetry records every fix attempt (outcome, strategy, round, tokens), enabling you to learn from past runs and optimize token spend. When the external paper proxy is running, sub-agent sessions are also captured out-of-band — no sweeper configuration required.

## Prerequisites

The `sweeper` binary must be in PATH. Build it if needed:

```bash
cd /path/to/sweeper && go build -o sweeper . && export PATH="$PWD:$PATH"
```

## Setup

If no `sweeper.md` exists in the working directory, gather this information:

1. **Target directory** - Which directory to lint (default: `.`)
2. **Lint command** - What linter to run (default: `golangci-lint run --out-format=line-number ./...`)
3. **Concurrency** - How many parallel sub-agents (default: `3`)
4. **Max rounds** - Retry rounds before stopping (default: `3`)
5. **VM mode** - Whether to isolate sub-agents in a stereOS VM (recommended for 5+ agents or sensitive repos)
6. **Constraints** - Files/directories off-limits, behavioral invariants to preserve

Then create a session document:

### `sweeper.md` - Living Session Document

```md
# Sweeper Session

**Started:** <ISO timestamp>
**Objective:** Fix all lint issues in `<target>`
**Linter:** `<command>`
**Concurrency:** <n> sub-agents
**Max rounds:** <n>
**VM:** <yes/no>
**Constraints:** <constraints>

## Status
- Round: 0
- Issues found: (pending first run)
- Issues fixed: 0
- Token spend: (pending — run `sweeper observe` after first run)

## What's Been Tried
(Updated after each round)

## Token Budget
(Updated via `sweeper observe` after each run)
```

Commit on a new branch: `sweeper/<goal>-<date>`

## Running Sweeper

Use the CLI to orchestrate the full loop. The CLI handles linting, parsing, parallel sub-agent dispatch, retry escalation, and telemetry. Session capture is handled out-of-band by the external paper proxy when it's running.

### Basic runs

```bash
# Default: golangci-lint with claude (default provider)
sweeper run

# Custom linter
sweeper run -- npm run lint

# Multi-round with escalation
sweeper run --max-rounds 3

# High concurrency in VM isolation
sweeper run --vm -c 5 --max-rounds 3 -- npx eslint --quiet .

# Preview what would be fixed
sweeper run --dry-run
```

### Alternative providers

```bash
# Use OpenAI Codex CLI instead of Claude
sweeper run --provider codex -- npm run lint

# Use a local Ollama model (no API key needed)
sweeper run --provider ollama --model qwen2.5-coder:7b

# Ollama with custom API base
sweeper run --provider ollama --model codellama --api-base http://gpu-server:11434
```

Available providers: `claude` (default, CLI), `codex` (CLI), `ollama` (API). CLI providers have built-in file tools. API providers include file content in the prompt and apply returned diffs. VM isolation (`--vm`) only works with CLI providers.

### VM isolation (recommended for production)

```bash
# Ephemeral VM — boots before sweep, tears down after
sweeper run --vm -- npx eslint --quiet .

# Reuse existing VM
sweeper run --vm-name my-vm -- cargo clippy 2>&1
```

Use `--vm` when:
- Running inside a Claude Code session (avoids nesting conflicts)
- Working with sensitive API keys (secrets stay in VM)
- High concurrency (dedicated resources)
- CI/CD (hermetic environment)

## How the CLI Orchestrates

Each `sweeper run` executes this loop:

1. **Lint**: Run linter command, parse structured output
2. **Group**: Issues grouped by file into parallel fix tasks
3. **Strategy**: Pick prompt strategy per file based on round + history:
   - **Round 0** (no prior history): `standard` — straightforward fix
   - **Round 1+** (any prior history): `retry` — different approach
   - **Consecutive stale >= threshold**: `exploration` — refactor surrounding code
   - **Stagnant after exploration**: file dropped
4. **Dispatch**: Parallel sub-agents fix each file (bounded by `--concurrency`)
5. **Record**: Each outcome (success, strategy, round, `prompt_tokens`/`output_tokens`) logged to `.sweeper/telemetry/` JSONL
6. **Re-lint**: Verify fixes, filter retryable issues
7. **Repeat or stop**: Continue if issues remain and rounds left

## Telemetry — The Learning Center

Sweeper's JSONL telemetry is the backbone for self-learning. Every fix attempt is recorded with its outcome and token usage.

### Check learned patterns

```bash
sweeper observe
```

This shows:
- **Success rate per linter** — which linters sweeper handles best
- **Round effectiveness** — which retry rounds contribute most fixes
- **Strategy effectiveness** — standard vs retry vs exploration success rates
- **Token usage per linter** — how much each linter costs to fix (aggregated from telemetry)

### Use telemetry data to make decisions

Before starting a sweep, check historical performance:

```bash
sweeper observe --target /path/to/project
```

Use the insights to tune your run:
- If round 1 fixes 90% of issues, `--max-rounds 1` saves tokens
- If exploration strategy has <10% success, lower `--stale-threshold` to skip stagnant files faster
- If a specific linter has low success rate, consider excluding those rules
- Compare token spend across runs to track improvement over time

### Token budget tracking

After each run, update `sweeper.md` with token spend from `sweeper observe`:

```
## Token Budget
- Run 1: 45,230 prompt + 12,100 completion = 57,330 total
- Run 2: 31,000 prompt + 8,200 completion = 39,200 total (31% reduction)
- Trend: improving — retry prompts getting more targeted
```

The goal is to fix more issues with fewer tokens over time. Telemetry makes this measurable.

## Resume

If `sweeper.md` already exists when you start:
1. Read it to understand what's been tried and token spend so far
2. Run `sweeper observe` to check recent success patterns
3. Read git log for recent sweeper commits
4. Choose `--max-rounds` and `--concurrency` based on `sweeper observe` insights
5. Continue from where you left off

## Updating sweeper.md

After each `sweeper run` completes, update the session document:
1. Record round results (issues found/fixed/remaining)
2. Run `sweeper observe` and record token spend
3. Note which strategies worked and which files are stagnant
4. Commit the updated `sweeper.md`
