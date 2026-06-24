package config

import "time"

// TOMLConfig is the top-level config parsed from .sweeper/config.toml.
type TOMLConfig struct {
	Version   int             `toml:"version"`
	Run       RunConfig       `toml:"run"`
	Provider  ProviderConfig  `toml:"provider"`
	Telemetry TelemetryConfig `toml:"telemetry"`
	VM        VMSectionConfig `toml:"vm"`
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
	Name         string   `toml:"name"`
	Model        string   `toml:"model"`
	APIBase      string   `toml:"api_base"`

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
			Name: "claude",
		},
		Telemetry: TelemetryConfig{
			Backend: "jsonl",
			Dir:     ".sweeper/telemetry",
		},
	}
}

var TOMLConfigKeySet = map[string]bool{
	"version":                             true,
	"run.concurrency":                     true,
	"run.rate_limit":                      true,
	"run.max_rounds":                      true,
	"run.stale_threshold":                 true,
	"run.dry_run":                         true,
	"provider.name":                       true,
	"provider.model":                      true,
	"provider.api_base":                   true,
	"provider.allowed_tools":              true,
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
}
