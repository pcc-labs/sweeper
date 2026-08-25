package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePi installs a fake pi binary on PATH that echoes its args.
func fakePi(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi"), []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestPiExecutorRunsPrintMode(t *testing.T) {
	fakePi(t)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewPiExecutor(PiConfig{Model: "qwen38-27b"})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.HasPrefix(result.Output, "-p ") {
		t.Errorf("expected -p print mode flag first, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "--model qwen38-27b") {
		t.Errorf("expected --model flag passed to pi, got: %s", result.Output)
	}
	if result.Provider != "pi" || result.Model != "qwen38-27b" {
		t.Errorf("expected provider/model recorded, got %q/%q", result.Provider, result.Model)
	}
}

func TestPiExecutorOmitsModelFlagWhenUnset(t *testing.T) {
	fakePi(t)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewPiExecutor(PiConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if strings.Contains(result.Output, "--model") {
		t.Errorf("expected no --model flag when model unset, got: %s", result.Output)
	}
}

func TestPiExecutorPassesExtraArgs(t *testing.T) {
	fakePi(t)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := PiConfig{ExtraArgs: []string{"-e", "guardrails.ts"}}
	result := NewPiExecutor(cfg)(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "-e guardrails.ts") {
		t.Errorf("expected extra args passed through, got: %s", result.Output)
	}
}

func TestPiExecutorInheritsEnvironment(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"key=$ANTHROPIC_API_KEY\""
	if err := os.WriteFile(filepath.Join(dir, "pi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewPiExecutor(PiConfig{})(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "key=sk-test") {
		t.Errorf("expected pi to inherit provider credentials from env, got: %s", result.Output)
	}
}
