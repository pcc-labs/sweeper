// Package paper detects whether the paper CLI is available to wrap spawned
// sub-agents. Sweeper launches claude via `paper start claude`, so paper's
// gateway manages authentication and captures the session — no API token is
// inherited from the environment. This package only checks that the `paper`
// binary is present so sweeper can warn when capture won't happen; it never
// starts, stops, or vendors paperd.
package paper

import "os/exec"

// Binary is the paper CLI sweeper shells out to (via `paper start claude`).
const Binary = "paper"

// Status reports whether the paper CLI is available to wrap spawned agents.
type Status struct {
	Available bool
	Path      string
	Message   string
}

// Check reports whether the paper CLI is on PATH. It performs no network calls;
// `paper start` itself validates the daemon and auth health at launch time.
func Check() Status {
	if path, err := exec.LookPath(Binary); err == nil {
		return Status{Available: true, Path: path}
	}
	return Status{
		Available: false,
		Message: "paper CLI not found — sub-agents will run without capture. " +
			"Install paper and run `paper init` to capture sessions.",
	}
}
