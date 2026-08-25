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

func TestDecodeExtraArgs(t *testing.T) {
	src := `
[worker]
name = "pi"
extra_args = ["--no-extensions"]

[providers.pi]
extra_args = ["-e", "guardrails.ts"]
`
	tc := NewDefaultTOMLConfig()
	if _, err := toml.Decode(src, &tc); err != nil {
		t.Fatal(err)
	}
	if got := tc.Worker.ExtraArgs; len(got) != 1 || got[0] != "--no-extensions" {
		t.Errorf("expected worker.extra_args decoded, got %v", got)
	}
	if got := tc.Providers["pi"].ExtraArgs; len(got) != 2 || got[0] != "-e" {
		t.Errorf("expected providers.pi.extra_args decoded, got %v", got)
	}
}

func TestFromTOMLExtraArgsMerge(t *testing.T) {
	// worker.extra_args wins over provider.extra_args and the
	// [providers.<name>] fallback.
	tc := NewDefaultTOMLConfig()
	tc.Worker.Name = "pi"
	tc.Worker.ExtraArgs = []string{"--worker-arg"}
	tc.Provider.ExtraArgs = []string{"--provider-arg"}
	tc.Providers = map[string]ProviderEndpoint{
		"pi": {ExtraArgs: []string{"--endpoint-arg"}},
	}
	cfg := FromTOML(tc)
	if len(cfg.ProviderArgs) != 1 || cfg.ProviderArgs[0] != "--worker-arg" {
		t.Errorf("expected worker.extra_args to win, got %v", cfg.ProviderArgs)
	}
	if got := cfg.ProviderExtraArgs["pi"]; len(got) != 1 || got[0] != "--endpoint-arg" {
		t.Errorf("expected ProviderExtraArgs mapped from [providers], got %v", got)
	}
}

func TestFromTOMLExtraArgsFallsBackToProvidersSection(t *testing.T) {
	// No worker/provider extra_args set: the worker's own args come from
	// [providers.<worker.name>].
	tc := NewDefaultTOMLConfig()
	tc.Worker.Name = "pi"
	tc.Providers = map[string]ProviderEndpoint{
		"pi": {ExtraArgs: []string{"-e", "guardrails.ts"}},
	}
	cfg := FromTOML(tc)
	if len(cfg.ProviderArgs) != 2 || cfg.ProviderArgs[0] != "-e" {
		t.Errorf("expected worker extra_args to fall back to [providers.pi], got %v", cfg.ProviderArgs)
	}
}
