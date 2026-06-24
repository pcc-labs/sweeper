# Sweeper Telemetry Format

All sweeper implementations (Go CLI, Claude Code skill, Pi extension) produce the same JSONL event format in `.sweeper/telemetry/`.

## Events

### init

Emitted once per session start.

```json
{
  "timestamp": "2026-03-14T10:00:00Z",
  "type": "init",
  "data": {
    "name": "session-name",
    "linterCommand": "golangci-lint run ./...",
    "targetDir": ".",
    "maxRounds": 3,
    "staleThreshold": 2
  }
}
```

### fix_attempt

Emitted per file per round.

```json
{
  "timestamp": "2026-03-14T10:01:00Z",
  "type": "fix_attempt",
  "data": {
    "file": "server.go",
    "success": true,
    "round": 1,
    "strategy": "standard",
    "issues_before": 3,
    "issues_after": 0,
    "linter": "golangci-lint",
    "duration": "2.3s"
  }
}
```

### round_complete

Emitted after all files in a round are processed.

```json
{
  "timestamp": "2026-03-14T10:02:00Z",
  "type": "round_complete",
  "data": {
    "round": 1,
    "linter": "golangci-lint",
    "tasks": 5,
    "fixed": 4,
    "failed": 1
  }
}
```

## File Location

All implementations write to `.sweeper/telemetry/YYYY-MM-DD.jsonl` (date-named files, append-only).

All implementations read all `*.jsonl` files in the directory for analysis and session resume.

## Token Usage

Token usage is recorded directly in the JSONL telemetry: each `fix_attempt` event carries
`prompt_tokens` and `output_tokens` reported by the provider. `sweeper observe` aggregates
these per linter alongside fix outcomes — no external data source is required.

1. Read `.sweeper/telemetry/*.jsonl` for fix attempt outcomes and per-attempt tokens
2. Sum `prompt_tokens` + `output_tokens` per linter
3. Report combined insights: success rates + token spend

## Session Capture (Paper)

Full session transcripts are captured by the external **paper** gateway. When the `paper` CLI
is installed, sweeper launches each `claude` sub-agent via `paper start claude`, so paper
manages authentication and captures the session — sweeper passes no `ANTHROPIC_API_KEY` and
strips the Anthropic auth/proxy vars from the sub-agent's environment. Sweeper neither reads
nor writes the paper/tapes API. If paper is absent, sweeper runs `claude` directly under its
own login (no capture). The learning loop above runs entirely on sweeper's own JSONL telemetry.
