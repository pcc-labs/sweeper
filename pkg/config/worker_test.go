package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFromTOMLWorkerOverridesProvider(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Provider.Name = "claude"
	tc.Provider.Model = "claude-sonnet-5"
	tc.Provider.APIBase = "http://old:1"
	tc.Worker.Name = "ollama"
	tc.Worker.Model = "qwen2.5-coder:7b"
	tc.Worker.APIBase = "http://localhost:11434"
	cfg := FromTOML(tc)
	if cfg.Provider != "ollama" || cfg.ProviderModel != "qwen2.5-coder:7b" || cfg.ProviderAPI != "http://localhost:11434" {
		t.Errorf("expected worker section to win, got %q/%q/%q", cfg.Provider, cfg.ProviderModel, cfg.ProviderAPI)
	}
}

func TestFromTOMLWorkerPartialOverride(t *testing.T) {
	// Empty worker fields fall through to [provider].
	tc := NewDefaultTOMLConfig()
	tc.Provider.Name = "claude"
	tc.Provider.Model = "claude-sonnet-5"
	tc.Worker.Model = "claude-haiku-4-5" // only model overridden
	cfg := FromTOML(tc)
	if cfg.Provider != "claude" {
		t.Errorf("expected provider name from [provider], got %q", cfg.Provider)
	}
	if cfg.ProviderModel != "claude-haiku-4-5" {
		t.Errorf("expected model from [worker], got %q", cfg.ProviderModel)
	}
}

func TestFromTOMLEscalationLadder(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Worker.Escalation.Ladder = []string{"claude/claude-haiku-4-5", "claude/claude-sonnet-5"}
	cfg := FromTOML(tc)
	want := []string{"claude/claude-haiku-4-5", "claude/claude-sonnet-5"}
	if !reflect.DeepEqual(cfg.EscalationLadder, want) {
		t.Errorf("expected ladder %v, got %v", want, cfg.EscalationLadder)
	}
}

func TestLadderDefaultsEmpty(t *testing.T) {
	cfg := FromTOML(NewDefaultTOMLConfig())
	if len(cfg.EscalationLadder) != 0 {
		t.Errorf("expected no ladder by default, got %v", cfg.EscalationLadder)
	}
}

func TestEnvOverridesWorker(t *testing.T) {
	t.Setenv("SWEEPER_WORKER_NAME", "ollama")
	t.Setenv("SWEEPER_WORKER_MODEL", "qwen2.5-coder:7b")
	t.Setenv("SWEEPER_WORKER_API_BASE", "http://localhost:11434")
	t.Setenv("SWEEPER_WORKER_ESCALATION_LADDER", "claude/claude-haiku-4-5,claude/claude-sonnet-5")
	tc := NewDefaultTOMLConfig()
	applyEnvOverrides(&tc)
	if tc.Worker.Name != "ollama" || tc.Worker.Model != "qwen2.5-coder:7b" || tc.Worker.APIBase != "http://localhost:11434" {
		t.Errorf("expected env worker fields applied, got %+v", tc.Worker)
	}
	want := []string{"claude/claude-haiku-4-5", "claude/claude-sonnet-5"}
	if !reflect.DeepEqual(tc.Worker.Escalation.Ladder, want) {
		t.Errorf("expected env ladder %v, got %v", want, tc.Worker.Escalation.Ladder)
	}
}

// TestEnvWorkerBeatsEnvProviderWhenBothSet pins the ordering when both the
// new SWEEPER_WORKER_* and legacy SWEEPER_PROVIDER_* env vars are set: the
// more specific SWEEPER_WORKER_* value must win, even though
// SWEEPER_PROVIDER_* is now mirrored into tc.Worker.* as well.
func TestEnvWorkerBeatsEnvProviderWhenBothSet(t *testing.T) {
	t.Setenv("SWEEPER_PROVIDER_NAME", "env-provider")
	t.Setenv("SWEEPER_PROVIDER_MODEL", "env-provider-model")
	t.Setenv("SWEEPER_PROVIDER_API_BASE", "http://env-provider:1")
	t.Setenv("SWEEPER_WORKER_NAME", "env-worker")
	t.Setenv("SWEEPER_WORKER_MODEL", "env-worker-model")
	t.Setenv("SWEEPER_WORKER_API_BASE", "http://env-worker:2")

	tc := NewDefaultTOMLConfig()
	applyEnvOverrides(&tc)

	if tc.Worker.Name != "env-worker" {
		t.Errorf("expected SWEEPER_WORKER_NAME to win, got %q", tc.Worker.Name)
	}
	if tc.Worker.Model != "env-worker-model" {
		t.Errorf("expected SWEEPER_WORKER_MODEL to win, got %q", tc.Worker.Model)
	}
	if tc.Worker.APIBase != "http://env-worker:2" {
		t.Errorf("expected SWEEPER_WORKER_API_BASE to win, got %q", tc.Worker.APIBase)
	}

	// Sanity: the legacy [provider] section still reflects the env override
	// too (mirrored, not lost).
	if tc.Provider.Name != "env-provider" {
		t.Errorf("expected tc.Provider.Name to reflect SWEEPER_PROVIDER_NAME, got %q", tc.Provider.Name)
	}

	cfg := FromTOML(tc)
	if cfg.Provider != "env-worker" || cfg.ProviderModel != "env-worker-model" || cfg.ProviderAPI != "http://env-worker:2" {
		t.Errorf("expected final config to prefer worker env values, got %q/%q/%q", cfg.Provider, cfg.ProviderModel, cfg.ProviderAPI)
	}
}

func TestLoadTOMLWorkerSection(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from ~/.sweeper/config.toml
	dir := t.TempDir()
	dot := filepath.Join(dir, ".sweeper")
	if err := os.MkdirAll(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "version = 1\n\n[worker]\nname = \"ollama\"\nmodel = \"qwen2.5-coder:7b\"\n\n[worker.escalation]\nladder = [\"claude/claude-haiku-4-5\"]\n"
	if err := os.WriteFile(filepath.Join(dot, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	tc, err := LoadTOML(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Worker.Name != "ollama" || len(tc.Worker.Escalation.Ladder) != 1 {
		t.Errorf("expected worker section parsed, got %+v", tc.Worker)
	}
}
