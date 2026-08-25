package worker

import (
	"os"
	"os/exec"
	"strings"
)

// Capture modes select which telemetry gateway sub-agent sessions flow
// through. "auto" preserves the historical behavior: wrap with paper when
// its CLI is installed, bare otherwise.
const (
	CaptureModeAuto  = "auto"
	CaptureModePaper = "paper"
	CaptureModeTapes = "tapes"
	CaptureModeNone  = "none"
)

// DefaultTapesProxy is the tapes proxy's documented default listen address,
// used when capture.tapes_proxy is unset. `tapes start` with auto ports
// picks a different port — set capture.tapes_proxy to match it.
const DefaultTapesProxy = "http://localhost:8080"

// CaptureMode reports whether s names a valid capture mode ("" = auto).
func ValidCaptureMode(s string) bool {
	switch s {
	case "", CaptureModeAuto, CaptureModePaper, CaptureModeTapes, CaptureModeNone:
		return true
	}
	return false
}

// CaptureConfig selects the session-capture gateway for sub-agents.
type CaptureConfig struct {
	Mode       string // auto (default) | paper | tapes | none
	TapesProxy string // ANTHROPIC_BASE_URL injected in tapes mode; DefaultTapesProxy when empty
}

func (c CaptureConfig) proxyOrDefault() string {
	if c.TapesProxy != "" {
		return c.TapesProxy
	}
	return DefaultTapesProxy
}

// PaperBinary resolves the paper CLI: `paperctl` (current name) first, then
// the legacy `paper`. Returns the resolved path and whether one was found.
func PaperBinary() (string, bool) {
	if p, err := exec.LookPath("paperctl"); err == nil {
		return p, true
	}
	if p, err := exec.LookPath("paper"); err == nil {
		return p, true
	}
	return "", false
}

// envWith returns the parent environment with name set to value, replacing
// any existing entry. Used by tapes mode to point a pass-through-env
// executor (pi) at the tapes proxy.
func envWith(name, value string) []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent)+1)
	prefix := name + "="
	for _, kv := range parent {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, prefix+value)
}
