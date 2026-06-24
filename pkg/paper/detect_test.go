package paper

import (
	"strings"
	"testing"
)

func TestCheckEnabledWhenProxySet(t *testing.T) {
	t.Setenv(ProxyEnvVar, "http://localhost:5000")
	s := Check()
	if !s.Enabled {
		t.Fatal("expected Enabled=true when proxy env is set")
	}
	if s.ProxyURL != "http://localhost:5000" {
		t.Errorf("ProxyURL = %q, want http://localhost:5000", s.ProxyURL)
	}
	if s.Message != "" {
		t.Errorf("expected no message when enabled, got %q", s.Message)
	}
}

func TestCheckWarnsWhenProxyUnset(t *testing.T) {
	t.Setenv(ProxyEnvVar, "")
	s := Check()
	if s.Enabled {
		t.Fatal("expected Enabled=false when proxy env is unset")
	}
	if s.ProxyURL != "" {
		t.Errorf("expected empty ProxyURL, got %q", s.ProxyURL)
	}
	if !strings.Contains(s.Message, "paper init") {
		t.Errorf("expected message to mention `paper init`, got %q", s.Message)
	}
}
