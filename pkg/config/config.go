package config

import "time"

type Config struct {
	TargetDir         string
	Concurrency       int
	RateLimit         time.Duration // minimum delay between agent dispatches
	TelemetryDir      string
	DryRun            bool
	LintCommand       []string
	LinterName        string
	MaxRounds         int
	StaleThreshold    int
	VM                bool              // --vm: boot ephemeral stereOS VM
	VMName            string            // --vm-name: use existing VM (no managed lifecycle)
	VMJcard           string            // --vm-jcard: custom jcard.toml path
	Provider          string            // AI provider name (e.g. "claude", "codex", "ollama")
	ProviderModel     string            // model override for the provider
	ProviderAPI       string            // API base URL for API-only providers
	AdvisorProvider   string            // provider for the sweep-planning advisor ("" = disabled)
	AdvisorModel      string            // model for the sweep-planning advisor
	EscalationLadder  []string          // worker escalation rungs above the base model ("" entries invalid)
	ProviderEndpoints map[string]string // per-provider api_base from [providers.<name>], for rungs off the worker's provider
}

// MaxConcurrency is the hard ceiling for parallel sub-agents regardless of
// user-supplied flags. Keeps API volume within responsible limits.
const MaxConcurrency = 5

func Default() Config {
	return Config{
		TargetDir:      ".",
		Concurrency:    2,
		RateLimit:      2 * time.Second,
		TelemetryDir:   ".sweeper/telemetry",
		DryRun:         false,
		MaxRounds:      1,
		StaleThreshold: 2,
		Provider:       "claude",
	}
}

// ClampConcurrency enforces MaxConcurrency and returns the clamped value.
func ClampConcurrency(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxConcurrency {
		return MaxConcurrency
	}
	return n
}

// FromTOML converts a TOMLConfig into the runtime Config struct.
// Note: TargetDir is not populated from TOML and must be set by the caller
// (it comes from the --target CLI flag or defaults to ".").
func FromTOML(tc TOMLConfig) Config {
	rateLimit, err := tc.Run.ParseRateLimit()
	if err != nil {
		rateLimit = 2 * time.Second
	}
	name := firstNonEmpty(tc.Worker.Name, tc.Provider.Name)
	endpoints := make(map[string]string, len(tc.Providers))
	for prov, ep := range tc.Providers {
		endpoints[prov] = ep.APIBase
	}
	return Config{
		TargetDir:         ".",
		Concurrency:       ClampConcurrency(tc.Run.Concurrency),
		RateLimit:         rateLimit,
		TelemetryDir:      tc.Telemetry.Dir,
		DryRun:            tc.Run.DryRun,
		MaxRounds:         tc.Run.MaxRounds,
		StaleThreshold:    tc.Run.StaleThreshold,
		VM:                tc.VM.Enabled,
		VMName:            tc.VM.Name,
		VMJcard:           tc.VM.Jcard,
		Provider:          name,
		ProviderModel:     firstNonEmpty(tc.Worker.Model, tc.Provider.Model),
		ProviderAPI:       firstNonEmpty(tc.Worker.APIBase, tc.Provider.APIBase, endpoints[name]),
		AdvisorProvider:   tc.Advisor.Name,
		AdvisorModel:      tc.Advisor.Model,
		EscalationLadder:  tc.Worker.Escalation.Ladder,
		ProviderEndpoints: endpoints,
	}
}

// firstNonEmpty returns the first non-empty string, used to merge the
// [worker] section over its [provider] back-compat alias (and, for
// api_base, the [providers.<name>] fallback).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
