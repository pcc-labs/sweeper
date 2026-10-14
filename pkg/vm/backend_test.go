package vm

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestParseBackend(t *testing.T) {
	cases := map[string]Backend{
		"": BackendStereOS, "stereos": BackendStereOS, "stereOS": BackendStereOS,
		"smol": BackendSmol, "smolvm": BackendSmol, " SMOL ": BackendSmol,
	}
	for in, want := range cases {
		got, err := ParseBackend(in)
		if err != nil || got != want {
			t.Errorf("ParseBackend(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseBackend("firecracker"); err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestBackendBinary(t *testing.T) {
	if BackendStereOS.Binary() != "mb" || BackendSmol.Binary() != "smolvm" {
		t.Errorf("got %s / %s", BackendStereOS.Binary(), BackendSmol.Binary())
	}
}

func TestNewStereOSRejectsImage(t *testing.T) {
	if _, _, err := newMachine(Options{Backend: BackendStereOS, Image: "alpine"}, nil); err == nil {
		t.Error("stereOS should reject --vm-image")
	}
}

func TestNewAttachIsUnmanaged(t *testing.T) {
	for _, b := range Backends {
		m, managed, err := newMachine(Options{Backend: b, Name: "existing", HostDir: "/h"}, nil)
		if err != nil {
			t.Fatalf("%s: %v", b, err)
		}
		if managed {
			t.Errorf("%s: attached machine should be unmanaged", b)
		}
		if Name(m) != "existing" {
			t.Errorf("%s: Name = %q", b, Name(m))
		}
		if err := m.Shutdown(); err != nil {
			t.Errorf("%s: unmanaged shutdown should be a no-op: %v", b, err)
		}
	}
}

// Both backends must satisfy Machine.
var (
	_ Machine = (*VM)(nil)
	_ Machine = (*SmolVM)(nil)
)

func TestNewBootsManagedOnEachBackend(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cases := []struct {
		opts Options
		bin  string
	}{
		{Options{Backend: "", HostDir: "/h", JcardDir: t.TempDir()}, "mb"},
		{Options{Backend: BackendStereOS, HostDir: "/h", JcardDir: t.TempDir()}, "mb"},
		{Options{Backend: BackendSmol, HostDir: "/h"}, "smolvm"},
	}
	for _, c := range cases {
		var bins []string
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			bins = append(bins, name)
			return nil, nil
		}
		m, managed, err := newMachine(c.opts, runner)
		if err != nil {
			t.Fatalf("%q: %v", c.opts.Backend, err)
		}
		if !managed || !strings.HasPrefix(Name(m), "sweeper-") {
			t.Errorf("%q: managed=%v name=%q", c.opts.Backend, managed, Name(m))
		}
		for _, b := range bins {
			if b != c.bin {
				t.Errorf("%q: shelled out to %s, want only %s", c.opts.Backend, b, c.bin)
			}
		}
	}
}

func TestNewBootFailures(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	fail := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("boom")
	}
	for _, b := range Backends {
		m, managed, err := newMachine(Options{Backend: b, HostDir: "/h", JcardDir: t.TempDir()}, fail)
		if err == nil || m != nil || managed {
			t.Errorf("%s: want error and no machine, got m=%v managed=%v err=%v", b, m, managed, err)
		}
	}
}

func TestNewUnknownBackend(t *testing.T) {
	if _, _, err := newMachine(Options{Backend: "firecracker"}, nil); err == nil {
		t.Error("expected error for unknown backend")
	}
}

type otherMachine struct{}

func (otherMachine) Exec(context.Context, ...string) ([]byte, error) { return nil, nil }
func (otherMachine) Shutdown() error                                 { return nil }
func (otherMachine) WorkspacePath(p string) string                   { return p }

func TestNameUnknownMachine(t *testing.T) {
	if got := Name(otherMachine{}); got != "" {
		t.Errorf("unknown machine type should have no name, got %q", got)
	}
}
