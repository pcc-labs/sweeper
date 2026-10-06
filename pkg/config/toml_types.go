package config

import "time"

// TOMLConfig is the top-level config parsed from .sweeper/config.toml.
type TOMLConfig struct {
	Version   int                         `toml:"version"`
	Run       RunConfig                   `toml:"run"`
	Provider  ProviderConfig              `toml:"provider"`
	Advisor   AdvisorConfig               `toml:"advisor"`
	Worker    WorkerConfig                `toml:"worker"`
	Providers map[string]ProviderEndpoint `toml:"providers"`
	Telemetry TelemetryConfig             `toml:"telemetry"`
	VM        VMSectionConfig             `toml:"vm"`
	Capture   CaptureSectionConfig        `toml:"capture"`
}

// CaptureSectionConfig selects the session-capture gateway for sub-agents:
// "auto" (default) wraps CLI agents with paperctl when installed, "paper"
// requires it, "tapes" points sub-agent traffic at the tapes proxy, "none"
// runs agents bare on their own login.
type CaptureSectionConfig struct {
	Mode       string `toml:"mode"`
	TapesProxy string `toml:"tapes_proxy"`
}

// ProviderEndpoint holds per-provider connection settings, keyed by provider
// name under [providers.<name>]. Consulted wherever an executor is built for
// that provider without a more specific api_base — notably escalation-ladder
// rungs on a provider other than the worker's.
type ProviderEndpoint struct {
	APIBase   string   `toml:"api_base"`
	ExtraArgs []string `toml:"extra_args"`
}

type RunConfig struct {
	Concurrency    int    `toml:"concurrency"`
	RateLimit      string `toml:"rate_limit"`
	MaxRounds      int    `toml:"max_rounds"`
	StaleThreshold int    `toml:"stale_threshold"`
	DryRun         bool   `toml:"dry_run"`
}

func (r RunConfig) ParseRateLimit() (time.Duration, error) {
	if r.RateLimit == "" {
		return 2 * time.Second, nil
	}
	return time.ParseDuration(r.RateLimit)
}

type ProviderConfig struct {
	Name      string   `toml:"name"`
	Model     string   `toml:"model"`
	APIBase   string   `toml:"api_base"`
	ExtraArgs []string `toml:"extra_args"`
}

// WorkerConfig configures the fix-executing worker role. It mirrors
// [provider] (which remains as a back-compat alias); non-empty worker
// fields win over the provider section.
type WorkerConfig struct {
	Name       string           `toml:"name"`
	Model      string           `toml:"model"`
	APIBase    string           `toml:"api_base"`
	ExtraArgs  []string         `toml:"extra_args"`
	Escalation EscalationConfig `toml:"escalation"`
}

// EscalationConfig defines the model escalation ladder: rungs above the
// base worker, tried in order when a file stagnates. Each entry is a model
// name, optionally prefixed with a registered provider ("claude/model");
// bare entries run on the worker's provider.
type EscalationConfig struct {
	Ladder []string `toml:"ladder"`
}

// AdvisorConfig configures the optional sweep-planning advisor. When name or
// model is set, a frontier model plans each round before workers dispatch.
type AdvisorConfig struct {
	Name  string `toml:"name"`  // provider name (claude, codex); defaults to "claude" when only model is set
	Model string `toml:"model"` // e.g. "claude-opus-4-8"
}

type TelemetryConfig struct {
	Backend   string          `toml:"backend"`
	Dir       string          `toml:"dir"`
	Confluent ConfluentConfig `toml:"confluent"`
}

type ConfluentConfig struct {
	Brokers        []string `toml:"brokers"`
	Topic          string   `toml:"topic"`
	ClientID       string   `toml:"client_id"`
	APIKeyEnv      string   `toml:"api_key_env"`
	APISecretEnv   string   `toml:"api_secret_env"`
	PublishTimeout string   `toml:"publish_timeout"`
}

type VMSectionConfig struct {
	Enabled bool   `toml:"enabled"`
	Name    string `toml:"name"`
	Jcard   string `toml:"jcard"`
	// Backend picks the VM implementation: "stereos" (default, via mb) or
	// "smol" (smolvm microVMs).
	Backend string `toml:"backend"`
	// Image is the OCI image the smol backend boots.
	Image string `toml:"image"`
}

func NewDefaultTOMLConfig() TOMLConfig {
	return TOMLConfig{
		Version: 1,
		Run: RunConfig{
			Concurrency:    2,
			RateLimit:      "2s",
			MaxRounds:      1,
			StaleThreshold: 2,
		},
		Provider: ProviderConfig{
			Name: "pi",
		},
		Telemetry: TelemetryConfig{
			Backend: "jsonl",
			Dir:     ".sweeper/telemetry",
		},
	}
}

var TOMLConfigKeySet = map[string]bool{
	"version":                  true,
	"run.concurrency":          true,
	"run.rate_limit":           true,
	"run.max_rounds":           true,
	"run.stale_threshold":      true,
	"run.dry_run":              true,
	"provider.name":            true,
	"provider.model":           true,
	"provider.api_base":        true,
	"provider.extra_args":      true,
	"provider.allowed_tools":   true,
	"advisor.name":             true,
	"advisor.model":            true,
	"worker.name":              true,
	"worker.model":             true,
	"worker.api_base":          true,
	"worker.extra_args":        true,
	"worker.escalation.ladder": true,
	// [providers.<name>] keys are dynamic (keyed by provider name) and
	// cannot be enumerated here; the section's leaf key is api_base.
	"telemetry.backend":                   true,
	"telemetry.dir":                       true,
	"telemetry.confluent.brokers":         true,
	"telemetry.confluent.topic":           true,
	"telemetry.confluent.client_id":       true,
	"telemetry.confluent.api_key_env":     true,
	"telemetry.confluent.api_secret_env":  true,
	"telemetry.confluent.publish_timeout": true,
	"vm.enabled":                          true,
	"vm.name":                             true,
	"vm.jcard":                            true,
	"vm.backend":                          true,
	"vm.image":                            true,
	"capture.mode":                        true,
	"capture.tapes_proxy":                 true,
}
