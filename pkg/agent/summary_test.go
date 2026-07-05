package agent

import "testing"

func TestFormatSummaryAllFixed(t *testing.T) {
	s := Summary{TotalIssues: 12, Tasks: 4, Fixed: 12, Failed: 0, Rounds: 1}
	got := FormatSummary(s, "")
	want := "Sweep complete: 12/12 issues fixed (1 round)."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSummaryPartialSuccess(t *testing.T) {
	s := Summary{TotalIssues: 2114, Tasks: 116, Fixed: 68, Failed: 99, Rounds: 1}
	got := FormatSummary(s, "")
	want := "Sweep complete: 68/2114 issues fixed; 99 tasks had unfixable issues (1 round)."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSummarySingleFailedTask(t *testing.T) {
	s := Summary{TotalIssues: 5, Tasks: 2, Fixed: 3, Failed: 1, Rounds: 2}
	got := FormatSummary(s, "")
	want := "Sweep complete: 3/5 issues fixed; 1 task had unfixable issues (2 rounds)."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSummaryWithTelemetryPath(t *testing.T) {
	s := Summary{TotalIssues: 10, Fixed: 4, Failed: 3, Rounds: 1}
	got := FormatSummary(s, ".sweeper/telemetry/2026-07-05.jsonl")
	want := "Sweep complete: 4/10 issues fixed; 3 tasks had unfixable issues (1 round).\nDetails: .sweeper/telemetry/2026-07-05.jsonl"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
