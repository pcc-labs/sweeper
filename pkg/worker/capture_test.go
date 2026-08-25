package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin installs a fake executable that echoes its argv and, when asked,
// selected env vars.
func fakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

const echoArgs = "#!/bin/sh\necho \"$@\"\n"
const echoEnv = "#!/bin/sh\necho \"base=$ANTHROPIC_BASE_URL key=$ANTHROPIC_API_KEY tok=$ANTHROPIC_AUTH_TOKEN\"\n"

func TestClaudeCaptureAutoPrefersPaperctl(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "paperctl", "#!/bin/sh\necho \"paperctl $@\"\n")
	fakeBin(t, dir, "paper", "#!/bin/sh\necho \"paper $@\"\n")
	fakeBin(t, dir, "claude", echoArgs)
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !strings.HasPrefix(result.Output, "paperctl start claude --") {
		t.Errorf("expected auto mode to wrap with paperctl first, got: %s", result.Output)
	}
}

func TestClaudeCaptureAutoFallsBackToLegacyPaper(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "paper", "#!/bin/sh\necho \"paper $@\"\n")
	fakeBin(t, dir, "claude", echoArgs)
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewClaudeExecutor(ClaudeConfig{})(context.Background(), task)
	if !strings.HasPrefix(result.Output, "paper start claude --") {
		t.Errorf("expected auto mode to fall back to legacy paper, got: %s", result.Output)
	}
}

func TestClaudeCaptureNoneSkipsPaperWrap(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "paperctl", "#!/bin/sh\necho \"paperctl $@\"\n")
	fakeBin(t, dir, "claude", echoArgs)
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := ClaudeConfig{Capture: CaptureConfig{Mode: CaptureModeNone}}
	result := NewClaudeExecutor(cfg)(context.Background(), task)
	if strings.Contains(result.Output, "paperctl") {
		t.Errorf("expected none mode to run claude bare, got: %s", result.Output)
	}
	if !strings.HasPrefix(result.Output, "--print") {
		t.Errorf("expected bare claude invocation, got: %s", result.Output)
	}
}

func TestClaudeCaptureTapesSetsProxyKeepsKeyStripped(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "paperctl", "#!/bin/sh\necho \"paperctl $@\"\n")
	fakeBin(t, dir, "claude", echoEnv)
	t.Setenv("PATH", dir)
	t.Setenv("ANTHROPIC_API_KEY", "sk-inherited")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-inherited")
	t.Setenv("ANTHROPIC_BASE_URL", "http://paperd:51539")

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := ClaudeConfig{Capture: CaptureConfig{Mode: CaptureModeTapes, TapesProxy: "http://127.0.0.1:38967"}}
	result := NewClaudeExecutor(cfg)(context.Background(), task)
	if !strings.Contains(result.Output, "base=http://127.0.0.1:38967") {
		t.Errorf("expected tapes proxy as base URL, got: %s", result.Output)
	}
	if strings.Contains(result.Output, "sk-inherited") {
		t.Errorf("expected inherited API key stripped, got: %s", result.Output)
	}
	if strings.Contains(result.Output, "tok-inherited") {
		t.Errorf("expected inherited auth token stripped, got: %s", result.Output)
	}
}

func TestClaudeCaptureTapesDefaultProxy(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "claude", echoEnv)
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := ClaudeConfig{Capture: CaptureConfig{Mode: CaptureModeTapes}}
	result := NewClaudeExecutor(cfg)(context.Background(), task)
	if !strings.Contains(result.Output, "base="+DefaultTapesProxy) {
		t.Errorf("expected default tapes proxy, got: %s", result.Output)
	}
}

func TestPiCapturePaperWrapsPaperctl(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "paperctl", "#!/bin/sh\necho \"paperctl $@\"\n")
	fakeBin(t, dir, "pi", echoArgs)
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := PiConfig{Capture: CaptureConfig{Mode: CaptureModePaper}}
	result := NewPiExecutor(cfg)(context.Background(), task)
	if !strings.HasPrefix(result.Output, "paperctl start pi -- -p") {
		t.Errorf("expected paper mode to wrap pi with paperctl, got: %s", result.Output)
	}
}

func TestPiCaptureAutoDoesNotWrap(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "paperctl", "#!/bin/sh\necho \"paperctl $@\"\n")
	fakeBin(t, dir, "pi", echoArgs)
	t.Setenv("PATH", dir)

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	result := NewPiExecutor(PiConfig{})(context.Background(), task)
	if strings.Contains(result.Output, "paperctl") {
		t.Errorf("expected auto mode to run pi bare (pi owns its providers), got: %s", result.Output)
	}
}

func TestPiCaptureTapesOverridesBaseURL(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "pi", "#!/bin/sh\necho \"base=$ANTHROPIC_BASE_URL key=$ANTHROPIC_API_KEY\"\n")
	t.Setenv("PATH", dir)
	t.Setenv("ANTHROPIC_API_KEY", "sk-kept")
	t.Setenv("ANTHROPIC_BASE_URL", "http://paperd:51539")

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := PiConfig{Capture: CaptureConfig{Mode: CaptureModeTapes, TapesProxy: "http://127.0.0.1:38967"}}
	result := NewPiExecutor(cfg)(context.Background(), task)
	if !strings.Contains(result.Output, "base=http://127.0.0.1:38967") {
		t.Errorf("expected tapes proxy to replace inherited base URL, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "key=sk-kept") {
		t.Errorf("expected pi to keep provider credentials in tapes mode, got: %s", result.Output)
	}
}

func TestValidCaptureMode(t *testing.T) {
	for _, ok := range []string{"", "auto", "paper", "tapes", "none"} {
		if !ValidCaptureMode(ok) {
			t.Errorf("expected %q valid", ok)
		}
	}
	if ValidCaptureMode("kafka") {
		t.Error("expected unknown mode invalid")
	}
}

func TestPiCapturePaperFallbackStripsEnv(t *testing.T) {
	// paper mode with no paperctl on PATH: the defensive fallback must keep
	// paper's contract and strip the inherited Anthropic auth env.
	dir := t.TempDir()
	fakeBin(t, dir, "pi", "#!/bin/sh\necho \"key=$ANTHROPIC_API_KEY tok=$ANTHROPIC_AUTH_TOKEN\"\n")
	t.Setenv("PATH", dir)
	t.Setenv("ANTHROPIC_API_KEY", "sk-inherited")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-inherited")

	task := Task{ID: 0, File: "test.go", Dir: t.TempDir(), Prompt: "fix it"}
	cfg := PiConfig{Capture: CaptureConfig{Mode: CaptureModePaper}}
	result := NewPiExecutor(cfg)(context.Background(), task)
	if strings.Contains(result.Output, "sk-inherited") || strings.Contains(result.Output, "tok-inherited") {
		t.Errorf("expected paper-mode fallback to strip Anthropic env, got: %s", result.Output)
	}
}
