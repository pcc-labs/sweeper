package config

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Concurrency != 2 {
		t.Errorf("expected default concurrency 2, got %d", cfg.Concurrency)
	}
	if cfg.RateLimit != 2*time.Second {
		t.Errorf("expected default rate limit 2s, got %s", cfg.RateLimit)
	}
	if cfg.TelemetryDir != ".sweeper/telemetry" {
		t.Errorf("unexpected telemetry dir: %s", cfg.TelemetryDir)
	}
}

func TestDefaultsEnablePaper(t *testing.T) {
	cfg := FromTOML(NewDefaultTOMLConfig())
	if !cfg.PaperEnabled {
		t.Error("paper capture detect+warn should be enabled by default")
	}
}

func TestDeprecatedNoTapesDisablesPaper(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Run.NoTapes = true
	cfg := FromTOML(tc)
	if cfg.PaperEnabled {
		t.Error("deprecated no_tapes=true should disable paper detect+warn")
	}
}

func TestDefaultMaxRounds(t *testing.T) {
	cfg := Default()
	if cfg.MaxRounds != 1 {
		t.Errorf("expected default MaxRounds 1, got %d", cfg.MaxRounds)
	}
}

func TestDefaultStaleThreshold(t *testing.T) {
	cfg := Default()
	if cfg.StaleThreshold != 2 {
		t.Errorf("expected default StaleThreshold 2, got %d", cfg.StaleThreshold)
	}
}

func TestClampConcurrency(t *testing.T) {
	if ClampConcurrency(0) != 1 {
		t.Error("should clamp 0 to 1")
	}
	if ClampConcurrency(3) != 3 {
		t.Error("should leave 3 unchanged")
	}
	if ClampConcurrency(100) != MaxConcurrency {
		t.Errorf("should clamp 100 to %d", MaxConcurrency)
	}
}

func TestDefaultConfigHasVMDisabled(t *testing.T) {
	cfg := Default()
	if cfg.VM {
		t.Error("VM should be disabled by default")
	}
	if cfg.VMName != "" {
		t.Error("VMName should be empty by default")
	}
	if cfg.VMJcard != "" {
		t.Error("VMJcard should be empty by default")
	}
}
