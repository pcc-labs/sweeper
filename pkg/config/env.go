package config

import (
	"os"
	"strconv"
	"strings"
)

// applyEnvOverrides reads SWEEPER_* environment variables and overlays
// them onto the TOMLConfig.
func applyEnvOverrides(tc *TOMLConfig) {
	if v := os.Getenv("SWEEPER_RUN_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tc.Run.Concurrency = n
		}
	}
	if v := os.Getenv("SWEEPER_RUN_RATE_LIMIT"); v != "" {
		tc.Run.RateLimit = v
	}
	if v := os.Getenv("SWEEPER_RUN_MAX_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tc.Run.MaxRounds = n
		}
	}
	if v := os.Getenv("SWEEPER_RUN_STALE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tc.Run.StaleThreshold = n
		}
	}
	if v := os.Getenv("SWEEPER_PROVIDER_NAME"); v != "" {
		tc.Provider.Name = v
	}
	if v := os.Getenv("SWEEPER_PROVIDER_MODEL"); v != "" {
		tc.Provider.Model = v
	}
	if v := os.Getenv("SWEEPER_PROVIDER_API_BASE"); v != "" {
		tc.Provider.APIBase = v
	}
if v := os.Getenv("SWEEPER_TELEMETRY_BACKEND"); v != "" {
		tc.Telemetry.Backend = v
	}
	if v := os.Getenv("SWEEPER_TELEMETRY_DIR"); v != "" {
		tc.Telemetry.Dir = v
	}
	if v := os.Getenv("SWEEPER_TELEMETRY_CONFLUENT_BROKERS"); v != "" {
		tc.Telemetry.Confluent.Brokers = strings.Split(v, ",")
	}
	if v := os.Getenv("SWEEPER_TELEMETRY_CONFLUENT_TOPIC"); v != "" {
		tc.Telemetry.Confluent.Topic = v
	}
	if v := os.Getenv("SWEEPER_TELEMETRY_CONFLUENT_CLIENT_ID"); v != "" {
		tc.Telemetry.Confluent.ClientID = v
	}
	if v := os.Getenv("SWEEPER_TELEMETRY_CONFLUENT_API_KEY_ENV"); v != "" {
		tc.Telemetry.Confluent.APIKeyEnv = v
	}
	if v := os.Getenv("SWEEPER_TELEMETRY_CONFLUENT_API_SECRET_ENV"); v != "" {
		tc.Telemetry.Confluent.APISecretEnv = v
	}
}
