package agent

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Override HOME so tests don't pick up the user's real ~/.sweeper config
	// or other home-directory state.
	dir, err := os.MkdirTemp("", "agent-test-home-*")
	if err == nil {
		_ = os.Setenv("HOME", dir)
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
