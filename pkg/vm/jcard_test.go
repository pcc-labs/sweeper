package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateJcard(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-123")
	dir := t.TempDir()
	path, err := GenerateJcard(dir, "sweeper-abc123", "/host/project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "jcard.toml") {
		t.Errorf("expected jcard.toml path, got %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "sweeper-abc123"`) {
		t.Error("jcard should contain VM name")
	}
	if !strings.Contains(content, "/host/project") {
		t.Error("jcard should contain host project dir")
	}
	if !strings.Contains(content, "/workspace") {
		t.Error("jcard should mount to /workspace in guest")
	}
	if !strings.Contains(content, "[secrets]") {
		t.Error("jcard should contain secrets section")
	}
	if !strings.Contains(content, "sk-test-key-123") {
		t.Error("jcard should contain ANTHROPIC_API_KEY value from env")
	}
}

func TestGenerateJcardNoAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	path, err := GenerateJcard(dir, "test", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, `ANTHROPIC_API_KEY = ""`) {
		t.Error("jcard should have empty API key when env var is unset")
	}
}

func TestGenerateJcardCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "vm")
	_, err := GenerateJcard(dir, "test-vm", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("GenerateJcard should create parent directories")
	}
}

func TestCleanupJcard(t *testing.T) {
	dir := t.TempDir()
	path, _ := GenerateJcard(dir, "sweeper-abc", "/tmp/proj")
	CleanupJcard(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("CleanupJcard should remove the file")
	}
}

func TestCleanupJcardNonexistent(t *testing.T) {
	// Should not panic on missing file
	CleanupJcard("/nonexistent/jcard.toml")
}

func TestGenerateJcardMkdirError(t *testing.T) {
	// Use a file as a path component so MkdirAll fails.
	base := t.TempDir()
	blockingFile := filepath.Join(base, "file")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(blockingFile, "nested")
	_, err := GenerateJcard(dir, "test-vm", "/tmp/proj")
	if err == nil {
		t.Error("expected error when MkdirAll fails")
	}
}

func TestGenerateJcardWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write to read-only dirs")
	}
	dir := t.TempDir()
	// Make dir read-only so WriteFile fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck
	_, err := GenerateJcard(dir, "test-vm", "/tmp/proj")
	if err == nil {
		t.Error("expected error when WriteFile fails")
	}
}

func TestMixtapeForArch(t *testing.T) {
	tests := []struct {
		goarch string
		want   string
	}{
		{"arm64", "coder-arm64:latest"},
		{"amd64", "coder-x86:latest"},
	}
	for _, tt := range tests {
		if got := mixtapeFor(tt.goarch); got != tt.want {
			t.Errorf("mixtapeFor(%q) = %q, want %q", tt.goarch, got, tt.want)
		}
	}
}

func TestGenerateJcardEmbedsArchMixtape(t *testing.T) {
	dir := t.TempDir()
	path, err := GenerateJcard(dir, "sweeper-test", "/tmp/project")
	if err != nil {
		t.Fatalf("GenerateJcard: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading jcard: %v", err)
	}
	want := fmt.Sprintf("mixtape = %q", mixtapeFor(runtime.GOARCH))
	if !strings.Contains(string(content), want) {
		t.Errorf("jcard missing %s\ngot:\n%s", want, content)
	}
	// The retired name must not come back: the registry no longer serves it.
	if strings.Contains(string(content), "opencode-mixtape") {
		t.Error("jcard still references the retired opencode-mixtape")
	}
}
