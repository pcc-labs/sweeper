# 🧹 Sweeper Agent

Multi-threaded code maintenance with resource isolated subagents and swappable AI providers.

Sweeper dispatches parallel AI agents to fix lint issues across your codebase, each running in its own isolated environment. Providers are swappable: use Claude Code (default), OpenAI Codex, or local models via Ollama. It groups issues by file, fans out concurrent fixes, escalates strategy when fixes stall, and records outcomes so it learns what works. With VM isolation enabled, each sub-agent runs inside a dedicated stereOS virtual machine with its own CPU, memory, and secrets boundary, safe to scale to 10+ concurrent agents.

```
                        sweeper run --vm -c 5
                              │
                    ┌─────────┼─────────┐
                    ▼         ▼         ▼
              ┌──────────────────────────────┐
              │        Worker Pool           │
              │ (rate-limited, max N=5)      │
              └──┬───┬───┬───┬───┬──────────┘
                 │   │   │   │   │
                 ▼   ▼   ▼   ▼   ▼
               ┌───┐┌───┐┌───┐┌───┐┌───┐
               │VM ││VM ││VM ││VM ││VM │  ◄── stereOS isolation
               │ 1 ││ 2 ││ 3 ││ 4 ││ 5 │      (secrets, CPU, memory)
               └─┬─┘└─┬─┘└─┬─┘└─┬─┘└─┬─┘
                 │     │     │     │     │
                 ▼     ▼     ▼     ▼     ▼
              claude  claude claude claude claude
              --print --print --print --print --print
                 │     │     │     │     │
                 └─────┴──┬──┴─────┴─────┘
                          │
                    ┌─────┼───────────┐
                    ▼     ▼           ▼
               streaming telemetry  paper
               progress  (.jsonl)  (capture)
```

Each sub-agent works on a single file. Results stream back as they complete, giving real-time progress instead of blocking until the entire round finishes.

## Why Sub-Agents

The main thread never reads or edits source files. It runs the linter, builds prompts, dispatches work, and collects results. All file-level reasoning happens inside sub-agents via `claude --print`, which are stateless, single-shot processes.

This matters because the orchestrator's context window stays small and predictable. It holds lint output, task metadata, and result summaries, not the contents of every file being fixed. A run that touches 50 files uses roughly the same orchestrator context as one that touches 5. The complexity scales in parallelism, not in context size.

```
  Orchestrator (main thread)              Sub-agents (disposable)
  ┌────────────────────────┐
  │ lint output            │              ┌──────────────────────┐
  │ file groupings         │  ──dispatch──▶ claude --print       │
  │ strategy decisions     │              │  reads auth.go       │
  │ result summaries       │  ◀──result── │  writes fix          │
  │                        │              └──────────────────────┘
  │ (never sees file       │              ┌──────────────────────┐
  │  contents directly)    │  ──dispatch──▶ claude --print       │
  │                        │              │  reads router.go     │
  │                        │  ◀──result── │  writes fix          │
  └────────────────────────┘              └──────────────────────┘
```

Sub-agents are fire-and-forget. Each one gets a prompt with the lint issues for its file, does the work, and exits. If it fails, the orchestrator knows from the exit code and can retry with an escalated strategy on the next round. No conversation state carries over between rounds, which keeps each attempt clean.

## Advisor Phase

With an advisor configured, each round starts with a one-shot planning call to a frontier model. The advisor receives the lint output and per-file round history — never file contents — and returns a structured plan: task ordering, per-file difficulty, and strategy hints. Workers then execute fixes in the planned order on whatever (cheaper) provider/model you configured. If the advisor fails, times out, or returns an unparseable plan, the round falls back to the mechanical file grouping and continues.

Every advisor call is recorded as an `advisor_plan` telemetry event, so `sweeper observe` can compare advised runs against mechanical ones.

## Model Escalation

With a `[worker.escalation]` ladder configured, files that stop improving climb to stronger models instead of just stronger prompts. The first round runs everything on the base worker (cheap, often local). When a file stagnates for `stale_threshold` rounds, sweeper switches it to the exploration prompt *and* moves it up one rung — e.g. `qwen2.5-coder:7b` → `claude-haiku-4-5` → `claude-sonnet-5`. A file is only abandoned after exploration has been tried at the top rung.

When both an advisor and a ladder are configured, the advisor is told the available tiers and can pin a gnarly file to a stronger starting rung via its `tier` hint.

A rung on the worker's own provider inherits the worker's `api_base`. To point a rung on a *different* provider at a non-default endpoint, add a `[providers.<name>]` section:

```toml
[worker]
name = "claude"

[worker.escalation]
ladder = ["ollama/qwen2.5-coder:32b", "claude/claude-sonnet-5"]

[providers.ollama]
api_base = "http://gpu-box:11434"
```

Rungs consult `[providers.<name>]` whenever a more specific `api_base` doesn't apply; without an entry, the provider's default endpoint is used (e.g. `localhost:11434` for Ollama). The section also serves as the worker's own endpoint when `worker.api_base` is unset.

Every `fix_attempt` telemetry event records the model and rung, and `sweeper observe` reports success rate and token spend per tier — so you learn which rung is cost-effective for which class of issue.

## Setup

### Go CLI (standalone)

The core binary. All integrations below (except Pi) require this.

```bash
go install github.com/papercomputeco/sweeper@latest
sweeper run                              # default: golangci-lint with claude
sweeper run --provider codex             # use OpenAI Codex CLI instead
sweeper run --provider ollama --model qwen2.5-coder:7b  # local model via Ollama
sweeper run --advisor-model claude-opus-4-8 --provider ollama --model qwen2.5-coder:7b  # frontier model plans, local model fixes
sweeper run --max-rounds 4                # with [worker.escalation] configured: stagnant files climb the model ladder
sweeper run --vm -c 3 --max-rounds 3    # VM isolation, 3 agents, 3 rounds
sweeper run -- npm run lint              # any linter
sweeper observe                          # review success rates + token spend
```

### Claude Code

To use sweeper as a skill in [Claude Code](https://docs.anthropic.com/en/docs/claude-code):

1. Build the binary:
```bash
go build -o sweeper .
export PATH="$PWD:$PATH"
```

2. Copy the skill into your project:
```bash
cp -r skills/sweeper/ /path/to/your-project/.claude/skills/sweeper/
```

3. Tell Claude: "Run sweeper on this project"

Claude will orchestrate `sweeper run` with the right flags based on your project.

> **Plugin support:** This repo includes a `.claude-plugin/` manifest for distribution as a Claude Code plugin, but it is not yet published to the plugin marketplace. If there is community interest, I am happy to submit it.

### opencode

> **Note:** opencode and Pi integrations have only been lightly tested. Claude Code is the primary development and testing target.

To use sweeper as a skill in [opencode](https://opencode.ai) (a terminal-based AI coding agent):

1. Build the binary:
```bash
go build -o sweeper .
export PATH="$PWD:$PATH"
```

2. Copy the skill into your project's agents directory:
```bash
mkdir -p /path/to/your-project/.opencode/agents/
cp skills/sweeper/SKILL.md /path/to/your-project/.opencode/agents/sweeper.md
```

3. Tell opencode: "Run sweeper on this project"

### Pi

[Pi](https://github.com/anthropics/pi) is a Claude-native IDE extension. Its sweeper integration reimplements the linting and telemetry loop in TypeScript using Pi's own tool system, so it does **not** need the Go binary.

```bash
pi install sweeper
```

This gives you `init_sweep`, `run_linter`, and `log_result` tools plus a dashboard widget. To start a sweep, tell Pi: "Sweep this project for lint issues"

## Providers

Sweeper supports swappable AI providers. Well-scoped tasks like lint fixes can run on smaller, cheaper models.

| Provider | Kind | Requires | Example |
|----------|------|----------|---------|
| `claude` (default) | CLI | `claude` CLI installed | `sweeper run` |
| `codex` | CLI | `codex` CLI installed | `sweeper run --provider codex` |
| `ollama` | API | Ollama running locally | `sweeper run --provider ollama --model qwen2.5-coder:7b` |

**CLI providers** (claude, codex) have built-in file tools. Sweeper sends a prompt and the harness reads/writes files directly.

**API providers** (ollama) are text-in, text-out. Sweeper includes file content in the prompt and applies the returned unified diff via `patch`.

### Provider flags

- `--provider <name>` — AI provider to use (default: `claude`)
- `--model <name>` — Model override for the provider (e.g. `qwen2.5-coder:7b` for ollama)
- `--api-base <url>` — API base URL for API providers (default: `http://localhost:11434` for ollama)

VM isolation (`--vm`) is only compatible with CLI providers.

## Examples

Sweeper works with any command that produces output, not just linters.

```bash
# Fix all golangci-lint issues (default)
sweeper run

# Fix ESLint issues across a JS/TS project
sweeper run -- npx eslint --quiet .

# Fix Clippy warnings in a Rust project
sweeper run -- cargo clippy 2>&1

# Run a custom script that checks for AI slop patterns
sweeper run -- ./scripts/check-slop.sh

# Higher concurrency with VM isolation
sweeper run --vm -c 5 --max-rounds 3 -- npm run lint

# Use Codex CLI
sweeper run --provider codex -- npm run lint

# Use a local Ollama model
sweeper run --provider ollama --model qwen2.5-coder:7b

# Ollama with a custom API base
sweeper run --provider ollama --model codellama --api-base http://gpu-server:11434
```

### Refactors

Sweeper fixes anything you can express as `file:line: message` output. Pipe the output of any detection command and agents will work on each file in parallel.

```bash
# Refactor files over 1000 lines
find . -name '*.go' -exec wc -l {} + \
  | awk '$1 > 1000 {print $2":1: file exceeds 1000 lines"}' \
  | sweeper run

# Resolve TODO comments across the codebase
grep -rn 'TODO\|FIXME\|HACK' --include='*.go' . | sweeper run

# Find and fix functions exceeding cyclomatic complexity
gocyclo -over 15 ./... | sweeper run

# Split oversized React components
find src -name '*.tsx' -exec wc -l {} + \
  | awk '$1 > 500 {print $2":1: component exceeds 500 lines"}' \
  | sweeper run

# Fix failing tests by feeding test output to agents
go test ./... 2>&1 | sweeper run
```

When using sweeper as a skill, you can pass arbitrary goals to the agent:

- "Run sweeper on this project" — default lint-fix loop
- "Run sweeper to clean up AI slop — remove verbose comments, unnecessary null checks, filler docstrings, and over-abstractions"
- "Run sweeper to fix all failing tests"
- "Run sweeper to migrate deprecated API calls"
- "Run sweeper to refactor files over 1000 lines"
- "Run sweeper to resolve all TODOs"

The agent will pick the right command and flags based on your goal.

## How It Works

This describes the Go CLI and skill-based integrations (Claude Code, opencode). Pi manages its own lint-fix loop through built-in tools and does not use the CLI.

1. **Lint**: run any linter, parse structured output (or fall back to raw mode)
2. **Plan**: group issues by file, pick strategy per file based on history
3. **Dispatch**: fan out to N concurrent sub-agents (default 2, max 5, rate-limited)
4. **Stream**: results arrive in real time as each file completes
5. **Escalate**: stalled files get retry prompts, then exploration prompts that consider surrounding code
6. **Record**: outcomes (success, strategy, round, tokens) logged to `.sweeper/telemetry/`
7. **Learn**: `sweeper observe` shows success rates by strategy, round, and linter

## Session Capture via Paper

When [paper](https://github.com/papercomputeco/paper) is installed, sweeper launches each `claude` sub-agent via `paper start claude`, so **paper's gateway manages authentication and captures the session** — sweeper passes no `ANTHROPIC_API_KEY` and inherits no API token (those vars are stripped from the sub-agent's environment). Run `paper init` once to bring up the daemon. If paper isn't installed, sweeper falls back to running `claude` directly under its own login (no capture).

The learning loop runs on sweeper's own telemetry (`.sweeper/telemetry/*.jsonl`), which records per-fix outcome, strategy, round, and token usage:

- **Token spend per linter**: know what each fix costs
- **Strategy effectiveness**: standard vs retry vs exploration success rates
- **Round effectiveness**: which retry rounds contribute most fixes
- **Trend tracking**: are you fixing more issues with fewer tokens over time?

Run `sweeper observe` after each sweep to see insights and tune your next run.

## Confluent Cloud Telemetry

Sweeper can stream telemetry events to Confluent Cloud alongside local JSONL files. Enable it in `.sweeper/config.toml`:

```toml
[telemetry]
backend = "confluent"
dir = ".sweeper/telemetry"

[telemetry.confluent]
brokers = ["pkc-xxxxx.region.provider.confluent.cloud:9092"]
topic = "sweeper.telemetry"
client_id = "sweeper"
api_key_env = "SWEEPER_CONFLUENT_API_KEY"
api_secret_env = "SWEEPER_CONFLUENT_API_SECRET"
```

Set `SWEEPER_CONFLUENT_API_KEY` and `SWEEPER_CONFLUENT_API_SECRET` in your environment. The config references env var names, not raw credentials.

For cluster and topic setup, install the [confluent-cloud-setup](https://github.com/papercomputeco/skills/tree/main/skills/confluent-cloud-setup) skill:

```bash
npx skills add papercomputeco/skills
```

## VM Isolation

Sub-agents can run inside ephemeral [stereOS](https://stereos.ai) virtual machines, managed by the `mb` (Masterblaster) CLI. This is what makes high concurrency safe.

Without VMs, sub-agents share the host process, filesystem, and API keys. At low concurrency (2-3) this works fine. At higher concurrency, you want each agent isolated so a runaway process or leaked credential stays contained.

With `--vm`, each sub-agent gets:

- **Own CPU and memory**: 4 cores, 8GB RAM per VM (configurable). No resource contention between agents.
- **Secret boundary**: `ANTHROPIC_API_KEY` is injected into the VM and never touches the host filesystem.
- **Nesting safety**: `claude --print` fails inside active Claude Code sessions due to nesting detection. VMs sidestep this entirely.
- **Clean teardown**: VMs are ephemeral. On exit (success, failure, or SIGINT), the VM is destroyed automatically.

```bash
sweeper run --vm -c 5 --max-rounds 3    # 5 isolated agents, 3 retry rounds
```

## Responsible Use

Sweeper dispatches automated Claude sub-agents. To stay within [Anthropic's usage policy](https://www.anthropic.com/legal/aup):

- **Rate limiting**: agents are dispatched with a configurable delay between each (default 2s, `--rate-limit`)
- **Concurrency cap**: hard maximum of 5 parallel agents regardless of flags
- **Skip permissions**: sub-agents use `--dangerously-skip-permissions` for non-interactive automated operation
- **Backoff**: exponential delay between retry rounds (5s, 10s, 20s, ... capped at 60s)
- **Agent identification**: all prompts identify the sub-agent as an automated tool with human oversight

A human must initiate every sweep and should review all changes before committing.

## Session State

Session state lives in `sweeper.md` for resume across restarts. The CLI generates this automatically, and the skill uses it to track progress and token spend.
