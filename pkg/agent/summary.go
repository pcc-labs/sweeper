package agent

import "fmt"

// FormatSummary renders the end-of-run summary. Unfixed issues are reported
// as partial success rather than an error, and telemetryPath (when non-empty)
// tells users where per-file details live.
func FormatSummary(s Summary, telemetryPath string) string {
	rounds := fmt.Sprintf("%d rounds", s.Rounds)
	if s.Rounds == 1 {
		rounds = "1 round"
	}

	line := fmt.Sprintf("Sweep complete: %d/%d issues fixed (%s).", s.Fixed, s.TotalIssues, rounds)
	if s.Failed > 0 {
		tasks := fmt.Sprintf("%d tasks", s.Failed)
		if s.Failed == 1 {
			tasks = "1 task"
		}
		line = fmt.Sprintf("Sweep complete: %d/%d issues fixed; %s had unfixable issues (%s).",
			s.Fixed, s.TotalIssues, tasks, rounds)
	}

	if telemetryPath != "" {
		line += "\nDetails: " + telemetryPath
	}
	return line
}
