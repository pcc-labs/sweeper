package provider

import "testing"

func TestParseRungBareModel(t *testing.T) {
	name, model := ParseRung("qwen2.5-coder:7b", "ollama")
	if name != "ollama" || model != "qwen2.5-coder:7b" {
		t.Errorf("expected ollama/qwen2.5-coder:7b, got %s/%s", name, model)
	}
}

func TestParseRungProviderPrefix(t *testing.T) {
	name, model := ParseRung("claude/claude-haiku-4-5", "ollama")
	if name != "claude" || model != "claude-haiku-4-5" {
		t.Errorf("expected claude/claude-haiku-4-5, got %s/%s", name, model)
	}
}

func TestParseRungUnregisteredPrefixIsModel(t *testing.T) {
	// "hf.co" is not a registered provider, so the whole entry is a model
	// name on the default provider.
	name, model := ParseRung("hf.co/some-model", "ollama")
	if name != "ollama" || model != "hf.co/some-model" {
		t.Errorf("expected ollama/hf.co/some-model, got %s/%s", name, model)
	}
}

func TestParseRungModelWithSlashOnPrefixedProvider(t *testing.T) {
	// Only the FIRST slash splits; the rest stays in the model.
	name, model := ParseRung("ollama/library/qwen", "claude")
	if name != "ollama" || model != "library/qwen" {
		t.Errorf("expected ollama/library/qwen, got %s/%s", name, model)
	}
}
