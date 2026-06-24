# Sweeper Skill

Autonomous lint-fix agent skill powered by the sweeper CLI. Orchestrates parallel sub-agents with swappable AI providers (Claude, Codex, Ollama), VM isolation, and telemetry-driven learning.

## Install in Claude Code

Copy the skill into your project:

```bash
cp -r skills/sweeper/ /path/to/your-project/.claude/skills/sweeper/
```

Then tell Claude: "Run sweeper on this project using ESLint"

## Install in opencode

Copy the skill into your project's agents directory:

```bash
mkdir -p /path/to/your-project/.opencode/agents/
cp skills/sweeper/SKILL.md /path/to/your-project/.opencode/agents/sweeper.md
```

## Install in Pi

```bash
pi install path/to/sweeper
```

Then tell Pi: "Run sweeper on this project"

## Prerequisites

The `sweeper` binary must be in PATH:

```bash
cd /path/to/sweeper && go build -o sweeper .
export PATH="/path/to/sweeper:$PATH"
```

For session capture through paper (optional):

```bash
paper init
```

This starts the external paper proxy and sets `ANTHROPIC_BASE_URL`; spawned sub-agents inherit it and are captured automatically. Sweeper requires no configuration for this — it only warns when the proxy env is missing.

## What It Does

1. Orchestrates `sweeper run` to dispatch parallel AI sub-agents (Claude, Codex, or Ollama)
2. Each sub-agent fixes a file's lint issues concurrently (bounded by `--concurrency`)
3. Retries with escalating strategies (standard -> retry -> exploration)
4. Swappable providers: `--provider claude` (default), `--provider codex`, `--provider ollama --model <name>`
5. Optional VM isolation via stereOS for security and resource isolation (CLI providers only)
6. Paper (when running) captures every sub-agent session out-of-band via the inherited proxy env
7. `sweeper observe` shows success rates, strategy effectiveness, and token spend
8. Session state tracked in `sweeper.md` for resume across restarts

## Telemetry — The Learning Center

Sweeper's own JSONL telemetry is the self-learning backbone. Every fix attempt is recorded, giving:

- Token usage per linter and strategy (from per-attempt `prompt_tokens`/`output_tokens`)
- Success rate trends over time
- Round/strategy effectiveness to optimize future runs
- Token budget tracking to reduce spend over time

Run `sweeper observe` after each sweep to see insights and tune your next run.
