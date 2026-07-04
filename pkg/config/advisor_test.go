package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromTOMLAdvisorFields(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Advisor.Name = "claude"
	tc.Advisor.Model = "claude-opus-4-8"
	cfg := FromTOML(tc)
	if cfg.AdvisorProvider != "claude" {
		t.Errorf("expected advisor provider claude, got %q", cfg.AdvisorProvider)
	}
	if cfg.AdvisorModel != "claude-opus-4-8" {
		t.Errorf("expected advisor model claude-opus-4-8, got %q", cfg.AdvisorModel)
	}
}

func TestAdvisorDefaultsEmpty(t *testing.T) {
	cfg := FromTOML(NewDefaultTOMLConfig())
	if cfg.AdvisorProvider != "" || cfg.AdvisorModel != "" {
		t.Errorf("expected advisor disabled by default, got provider=%q model=%q",
			cfg.AdvisorProvider, cfg.AdvisorModel)
	}
}

func TestEnvOverridesAdvisor(t *testing.T) {
	t.Setenv("SWEEPER_ADVISOR_NAME", "codex")
	t.Setenv("SWEEPER_ADVISOR_MODEL", "o4")
	tc := NewDefaultTOMLConfig()
	applyEnvOverrides(&tc)
	if tc.Advisor.Name != "codex" {
		t.Errorf("expected env advisor name codex, got %q", tc.Advisor.Name)
	}
	if tc.Advisor.Model != "o4" {
		t.Errorf("expected env advisor model o4, got %q", tc.Advisor.Model)
	}
}

func TestLoadTOMLAdvisorSection(t *testing.T) {
	dir := t.TempDir()
	dotdir := filepath.Join(dir, ".sweeper")
	if err := os.MkdirAll(dotdir, 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "version = 1\n\n[advisor]\nname = \"claude\"\nmodel = \"claude-opus-4-8\"\n"
	if err := os.WriteFile(filepath.Join(dotdir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	tc, err := LoadTOML(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Advisor.Name != "claude" || tc.Advisor.Model != "claude-opus-4-8" {
		t.Errorf("expected advisor section parsed, got %+v", tc.Advisor)
	}
}
