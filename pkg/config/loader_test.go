package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromTOMLFile(t *testing.T) {
	dir := t.TempDir()
	sweeper := filepath.Join(dir, ".sweeper")
	if err := os.MkdirAll(sweeper, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := `
version = 1

[run]
concurrency = 4
max_rounds = 3

[provider]
name = "codex"

[telemetry]
backend = "confluent"

[telemetry.confluent]
brokers = ["broker1:9092", "broker2:9092"]
topic = "sweeper.events"
`
	if err := os.WriteFile(filepath.Join(sweeper, "config.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	tc, err := LoadTOML(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Run.Concurrency != 4 {
		t.Errorf("expected concurrency 4, got %d", tc.Run.Concurrency)
	}
	if tc.Run.MaxRounds != 3 {
		t.Errorf("expected max_rounds 3, got %d", tc.Run.MaxRounds)
	}
	if tc.Provider.Name != "codex" {
		t.Errorf("expected provider codex, got %s", tc.Provider.Name)
	}
	if tc.Telemetry.Backend != "confluent" {
		t.Errorf("expected backend confluent, got %s", tc.Telemetry.Backend)
	}
	if len(tc.Telemetry.Confluent.Brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(tc.Telemetry.Confluent.Brokers))
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	tc, err := LoadTOML(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Run.Concurrency != 2 {
		t.Errorf("expected default concurrency 2, got %d", tc.Run.Concurrency)
	}
	if tc.Provider.Name != "claude" {
		t.Errorf("expected default provider claude, got %s", tc.Provider.Name)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("SWEEPER_PROVIDER_NAME", "ollama")
	t.Setenv("SWEEPER_RUN_CONCURRENCY", "5")
	tc, err := LoadTOML(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Provider.Name != "ollama" {
		t.Errorf("expected provider ollama from env, got %s", tc.Provider.Name)
	}
	if tc.Run.Concurrency != 5 {
		t.Errorf("expected concurrency 5 from env, got %d", tc.Run.Concurrency)
	}
}

func TestLoadExplicitConfigPath(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
version = 1

[run]
concurrency = 3
`
	configPath := filepath.Join(dir, "custom-config.toml")
	if err := os.WriteFile(configPath, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	tc, err := LoadTOML("", configPath)
	if err != nil {
		t.Fatal(err)
	}
	if tc.Run.Concurrency != 3 {
		t.Errorf("expected concurrency 3, got %d", tc.Run.Concurrency)
	}
}

func TestFromTOML(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Run.Concurrency = 4
	tc.Provider.Name = "codex"
	tc.Telemetry.Dir = "/tmp/tel"
	cfg := FromTOML(tc)
	if cfg.Concurrency != 4 {
		t.Errorf("expected concurrency 4, got %d", cfg.Concurrency)
	}
	if cfg.Provider != "codex" {
		t.Errorf("expected provider codex, got %s", cfg.Provider)
	}
	if cfg.TelemetryDir != "/tmp/tel" {
		t.Errorf("expected telemetry dir /tmp/tel, got %s", cfg.TelemetryDir)
	}
}

func TestFromTOMLInvalidRateLimit(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Run.RateLimit = "not-a-duration"
	cfg := FromTOML(tc)
	// Should fall back to 2s default when parsing fails
	if cfg.RateLimit != 2*1000*1000*1000*2 {
		// compare via time package
	}
	if cfg.RateLimit.String() != "2s" {
		t.Errorf("expected fallback rate limit 2s, got %s", cfg.RateLimit)
	}
}


func TestFromTOMLVMFields(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.VM.Enabled = true
	tc.VM.Name = "myvm"
	tc.VM.Jcard = "/path/to/jcard.toml"
	tc.Provider.Model = "gpt-4"
	tc.Provider.APIBase = "https://api.example.com"
	tc.Run.DryRun = true
	disabled := false
	tc.Paper.Enabled = &disabled
	tc.Run.MaxRounds = 5
	tc.Run.StaleThreshold = 3
	cfg := FromTOML(tc)
	if !cfg.VM {
		t.Error("expected VM enabled")
	}
	if cfg.VMName != "myvm" {
		t.Errorf("expected VMName myvm, got %s", cfg.VMName)
	}
	if cfg.VMJcard != "/path/to/jcard.toml" {
		t.Errorf("expected VMJcard /path/to/jcard.toml, got %s", cfg.VMJcard)
	}
	if cfg.ProviderModel != "gpt-4" {
		t.Errorf("expected ProviderModel gpt-4, got %s", cfg.ProviderModel)
	}
	if cfg.ProviderAPI != "https://api.example.com" {
		t.Errorf("expected ProviderAPI, got %s", cfg.ProviderAPI)
	}
	if !cfg.DryRun {
		t.Error("expected DryRun true")
	}
	if cfg.PaperEnabled {
		t.Error("expected PaperEnabled false when paper.enabled=false")
	}
	if cfg.MaxRounds != 5 {
		t.Errorf("expected MaxRounds 5, got %d", cfg.MaxRounds)
	}
	if cfg.StaleThreshold != 3 {
		t.Errorf("expected StaleThreshold 3, got %d", cfg.StaleThreshold)
	}
}

func TestApplyEnvOverridesAllFields(t *testing.T) {
	t.Setenv("SWEEPER_RUN_CONCURRENCY", "3")
	t.Setenv("SWEEPER_RUN_RATE_LIMIT", "500ms")
	t.Setenv("SWEEPER_RUN_MAX_ROUNDS", "5")
	t.Setenv("SWEEPER_RUN_STALE_THRESHOLD", "4")
	t.Setenv("SWEEPER_PROVIDER_NAME", "codex")
	t.Setenv("SWEEPER_PROVIDER_MODEL", "gpt-4o")
	t.Setenv("SWEEPER_PROVIDER_API_BASE", "https://api.openai.com")
	t.Setenv("SWEEPER_PROVIDER_ALLOWED_TOOLS", "Read,Write")
	t.Setenv("SWEEPER_PAPER_ENABLED", "false")
	t.Setenv("SWEEPER_TELEMETRY_BACKEND", "confluent")
	t.Setenv("SWEEPER_TELEMETRY_DIR", "/tmp/tel")
	t.Setenv("SWEEPER_TELEMETRY_CONFLUENT_BROKERS", "b1:9092,b2:9092")
	t.Setenv("SWEEPER_TELEMETRY_CONFLUENT_TOPIC", "my-topic")
	t.Setenv("SWEEPER_TELEMETRY_CONFLUENT_CLIENT_ID", "sweeper-client")
	t.Setenv("SWEEPER_TELEMETRY_CONFLUENT_API_KEY_ENV", "MY_API_KEY")
	t.Setenv("SWEEPER_TELEMETRY_CONFLUENT_API_SECRET_ENV", "MY_API_SECRET")

	tc := NewDefaultTOMLConfig()
	applyEnvOverrides(&tc)

	if tc.Run.Concurrency != 3 {
		t.Errorf("expected concurrency 3, got %d", tc.Run.Concurrency)
	}
	if tc.Run.RateLimit != "500ms" {
		t.Errorf("expected rate_limit 500ms, got %s", tc.Run.RateLimit)
	}
	if tc.Run.MaxRounds != 5 {
		t.Errorf("expected max_rounds 5, got %d", tc.Run.MaxRounds)
	}
	if tc.Run.StaleThreshold != 4 {
		t.Errorf("expected stale_threshold 4, got %d", tc.Run.StaleThreshold)
	}
	if tc.Provider.Name != "codex" {
		t.Errorf("expected provider codex, got %s", tc.Provider.Name)
	}
	if tc.Provider.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", tc.Provider.Model)
	}
	if tc.Provider.APIBase != "https://api.openai.com" {
		t.Errorf("expected api_base, got %s", tc.Provider.APIBase)
	}
	if tc.Paper.Enabled == nil || *tc.Paper.Enabled {
		t.Error("expected paper.enabled false from SWEEPER_PAPER_ENABLED=false")
	}
	if tc.Telemetry.Backend != "confluent" {
		t.Errorf("expected backend confluent, got %s", tc.Telemetry.Backend)
	}
	if tc.Telemetry.Dir != "/tmp/tel" {
		t.Errorf("expected telemetry dir /tmp/tel, got %s", tc.Telemetry.Dir)
	}
	if len(tc.Telemetry.Confluent.Brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(tc.Telemetry.Confluent.Brokers))
	}
	if tc.Telemetry.Confluent.Topic != "my-topic" {
		t.Errorf("expected topic my-topic, got %s", tc.Telemetry.Confluent.Topic)
	}
	if tc.Telemetry.Confluent.ClientID != "sweeper-client" {
		t.Errorf("expected client_id sweeper-client, got %s", tc.Telemetry.Confluent.ClientID)
	}
	if tc.Telemetry.Confluent.APIKeyEnv != "MY_API_KEY" {
		t.Errorf("expected api_key_env MY_API_KEY, got %s", tc.Telemetry.Confluent.APIKeyEnv)
	}
	if tc.Telemetry.Confluent.APISecretEnv != "MY_API_SECRET" {
		t.Errorf("expected api_secret_env MY_API_SECRET, got %s", tc.Telemetry.Confluent.APISecretEnv)
	}
}

func TestLoadExplicitConfigPathNotFound(t *testing.T) {
	_, err := LoadTOML("", "/nonexistent/path/config.toml")
	if err == nil {
		t.Error("expected error for missing explicit config path, got nil")
	}
}

func TestLoadHomeConfig(t *testing.T) {
	// Create a fake home dir with .sweeper/config.toml
	fakeHome := t.TempDir()
	sweeper := filepath.Join(fakeHome, ".sweeper")
	if err := os.MkdirAll(sweeper, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := `
version = 1

[provider]
name = "ollama"
`
	if err := os.WriteFile(filepath.Join(sweeper, "config.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// Point HOME at the fake home dir so os.UserHomeDir() returns it.
	t.Setenv("HOME", fakeHome)

	// Use a project dir with no .sweeper so only home config loads
	projectDir := t.TempDir()
	tc, err := LoadTOML(projectDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Provider.Name != "ollama" {
		t.Errorf("expected provider ollama from home config, got %s", tc.Provider.Name)
	}
}
