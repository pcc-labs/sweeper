package paper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAvailableWhenPaperOnPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Binary), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	s := Check()
	if !s.Available {
		t.Fatal("expected Available=true when paper is on PATH")
	}
	if s.Path == "" {
		t.Error("expected resolved Path when paper is available")
	}
	if s.Message != "" {
		t.Errorf("expected no message when available, got %q", s.Message)
	}
}

func TestCheckWarnsWhenPaperMissing(t *testing.T) {
	// Empty PATH dir: no paper binary resolvable.
	t.Setenv("PATH", t.TempDir())
	s := Check()
	if s.Available {
		t.Fatal("expected Available=false when paper is not on PATH")
	}
	if s.Path != "" {
		t.Errorf("expected empty Path when unavailable, got %q", s.Path)
	}
	if !strings.Contains(s.Message, "paper init") {
		t.Errorf("expected message to mention `paper init`, got %q", s.Message)
	}
}
