package config

import (
	"github.com/BurntSushi/toml"
	"testing"
)

func TestDecodeProvidersSection(t *testing.T) {
	src := `
[worker]
name = "claude"

[worker.escalation]
ladder = ["ollama/qwen2.5-coder:32b"]

[providers.ollama]
api_base = "http://gpu-box:11434"
`
	tc := NewDefaultTOMLConfig()
	if _, err := toml.Decode(src, &tc); err != nil {
		t.Fatal(err)
	}
	if got := tc.Providers["ollama"].APIBase; got != "http://gpu-box:11434" {
		t.Errorf("expected providers.ollama.api_base decoded, got %q", got)
	}
}

func TestFromTOMLProviderEndpoints(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Providers = map[string]ProviderEndpoint{
		"ollama": {APIBase: "http://gpu-box:11434"},
	}
	cfg := FromTOML(tc)
	if got := cfg.ProviderEndpoints["ollama"]; got != "http://gpu-box:11434" {
		t.Errorf("expected ProviderEndpoints mapped from [providers], got %q", got)
	}
}

func TestFromTOMLWorkerAPIBaseFallsBackToProvidersSection(t *testing.T) {
	// No worker/provider api_base set: the worker's own endpoint comes from
	// [providers.<worker.name>].
	tc := NewDefaultTOMLConfig()
	tc.Worker.Name = "ollama"
	tc.Providers = map[string]ProviderEndpoint{
		"ollama": {APIBase: "http://gpu-box:11434"},
	}
	cfg := FromTOML(tc)
	if cfg.ProviderAPI != "http://gpu-box:11434" {
		t.Errorf("expected worker api_base to fall back to [providers.ollama], got %q", cfg.ProviderAPI)
	}
}

func TestFromTOMLWorkerAPIBaseBeatsProvidersSection(t *testing.T) {
	tc := NewDefaultTOMLConfig()
	tc.Worker.Name = "ollama"
	tc.Worker.APIBase = "http://localhost:11434"
	tc.Providers = map[string]ProviderEndpoint{
		"ollama": {APIBase: "http://gpu-box:11434"},
	}
	cfg := FromTOML(tc)
	if cfg.ProviderAPI != "http://localhost:11434" {
		t.Errorf("expected worker.api_base to win over [providers.ollama], got %q", cfg.ProviderAPI)
	}
}
