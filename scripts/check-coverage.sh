#!/usr/bin/env bash
#
# check-coverage.sh — enforce 100% test coverage on pkg/ packages.
#
# Excluded from measurement:
#   - cmd/ and main.go (CLI wiring, like pokemon excludes observe_cli.py)
#   - pkg/worker/claude.go (shells out to external 'claude' binary)
#   - Specific functions with SQL error paths untriggerable via SQLite driver
#
# Usage: scripts/check-coverage.sh

set -euo pipefail

COVERFILE=$(mktemp /tmp/coverage-XXXXXX.out)
trap 'rm -f "$COVERFILE"' EXIT

echo "Running tests with coverage..."
go test -coverprofile="$COVERFILE" ./pkg/... > /dev/null 2>&1

# Files excluded entirely (external binary integrations).
EXCLUDED_FILES="claude.go|codex.go"

# Functions excluded from 100% check.
# These contain defensive SQL rows.Scan/db.Ping error paths that are
# impossible to trigger with SQLite's permissive type coercion driver,
# or shell out to external binaries (mb CLI for stereOS VMs).
EXCLUDED_FUNCTIONS=(
  "pkg/vm/vm.go:.*defaultRunner"
  "pkg/vm/vm.go:.*Boot"
  "pkg/vm/vm.go:.*Attach"
  "pkg/worker/ollama.go:.*NewOllamaExecutor"
  "pkg/worker/ollama.go:.*ollamaChat"
  "pkg/worker/ollama.go:.*applyDiff"
  "pkg/provider/codex.go:.*init"
  "pkg/provider/ollama.go:.*init"
  "pkg/linter/linter.go:.*normalizeIssuePaths"
  "pkg/dotdir/manager.go:.*Target"
  "pkg/dotdir/manager.go:.*TargetIn"
  "pkg/telemetry/jsonl.go:.*NewPublisher"
  "pkg/telemetry/confluent/confluent.go:.*NewPublisher"
  "pkg/telemetry/confluent/confluent.go:.*Publish"
)

# Build grep exclusion pattern.
EXCLUDE_PATTERN="$EXCLUDED_FILES"
for func in "${EXCLUDED_FUNCTIONS[@]}"; do
  EXCLUDE_PATTERN="$EXCLUDE_PATTERN|$func"
done

# Check for functions below 100% (excluding allowed ones).
UNCOVERED=$(go tool cover -func="$COVERFILE" \
  | grep "^github.com/papercomputeco/sweeper/pkg/" \
  | grep -v "100.0%" \
  | grep -v "total:" \
  | grep -Ev "$EXCLUDE_PATTERN" || true)

if [ -n "$UNCOVERED" ]; then
  echo "FAIL: Functions below 100% coverage:"
  echo "$UNCOVERED"
  echo ""
  echo "Either add tests or add the function to the exclusion list in scripts/check-coverage.sh"
  exit 1
fi

# Show summary.
TOTAL=$(go tool cover -func="$COVERFILE" | grep "total:" | awk '{print $3}')
echo "PASS: Coverage check passed (total: $TOTAL)"
