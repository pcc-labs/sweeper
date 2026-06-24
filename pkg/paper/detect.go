// Package paper detects whether spawned sub-agents are being routed through the
// paper proxy. Capture itself requires no code: child `claude` processes inherit
// ANTHROPIC_BASE_URL from the environment, which points at the external paperd
// proxy started by `paper init`. This package only observes that wiring so sweeper
// can warn when it is missing — it never starts, stops, or vendors paperd.
package paper

import "os"

// ProxyEnvVar is the environment variable spawned agents inherit to reach the
// paper proxy. When it is set, sessions flow through paper for capture.
const ProxyEnvVar = "ANTHROPIC_BASE_URL"

// Status reports whether the paper proxy environment is wired up.
type Status struct {
	Enabled  bool
	ProxyURL string
	Message  string
}

// Check reports the paper capture status based purely on the proxy environment
// variable. It performs no network calls.
func Check() Status {
	if url := os.Getenv(ProxyEnvVar); url != "" {
		return Status{Enabled: true, ProxyURL: url}
	}
	return Status{
		Enabled: false,
		Message: ProxyEnvVar + " not set — spawned agents won't be captured by paper. " +
			"Run `paper init` to start the proxy.",
	}
}
