package config

import (
	"testing"
	"time"
)

func TestNewDefaultTOMLConfig(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	if tc.Version != 1 {
		t.Errorf("expected version 1, got %d", tc.Version)
	}
	if tc.Run.Concurrency != 2 {
		t.Errorf("expected concurrency 2, got %d", tc.Run.Concurrency)
	}
	if tc.Run.RateLimit != "2s" {
		t.Errorf("expected rate_limit 2s, got %s", tc.Run.RateLimit)
	}
	if tc.Provider.Name != "pi" {
		t.Errorf("expected provider pi, got %s", tc.Provider.Name)
	}
	if tc.Telemetry.Backend != "jsonl" {
		t.Errorf("expected telemetry backend jsonl, got %s", tc.Telemetry.Backend)
	}
}

func TestTOMLConfigKeySet(t *testing.T) {
	if !TOMLConfigKeySet["run.concurrency"] {
		t.Error("missing key run.concurrency")
	}
	if !TOMLConfigKeySet["telemetry.confluent.brokers"] {
		t.Error("missing key telemetry.confluent.brokers")
	}
	if !TOMLConfigKeySet["provider.name"] {
		t.Error("missing key provider.name")
	}
}

func TestRunConfigParseDuration(t *testing.T) {
	rc := RunConfig{RateLimit: "500ms"}
	d, err := rc.ParseRateLimit()
	if err != nil {
		t.Fatal(err)
	}
	if d != 500*time.Millisecond {
		t.Errorf("expected 500ms, got %s", d)
	}
}

func TestRunConfigParseRateLimitEmpty(t *testing.T) {
	rc := RunConfig{}
	d, err := rc.ParseRateLimit()
	if err != nil {
		t.Fatal(err)
	}
	if d != 2*time.Second {
		t.Errorf("expected 2s default, got %s", d)
	}
}
