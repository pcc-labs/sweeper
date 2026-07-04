package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/worker"
)

// fakeCLI installs a fake binary on PATH that echoes its args, so tests can
// verify what flags a provider's executor passes to the underlying CLI.
func fakeCLI(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho \"$@\""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// Regression test for #20: --model was silently dropped by the claude and
// codex providers because NewExec ignored provider.Config.
func TestCLIProvidersForwardModel(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			fakeCLI(t, name)

			p, err := Get(name)
			if err != nil {
				t.Fatal(err)
			}
			exec := p.NewExec(Config{Model: "test-model-x"})
			task := worker.Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
			result := exec(context.Background(), task)
			if !result.Success {
				t.Fatalf("expected success, got error: %s", result.Error)
			}
			if !strings.Contains(result.Output, "--model test-model-x") {
				t.Errorf("expected provider to forward model to CLI, got: %s", result.Output)
			}
			if result.Model != "test-model-x" {
				t.Errorf("expected result.Model recorded, got %q", result.Model)
			}
		})
	}
}
