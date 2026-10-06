package vm

import (
	"context"
	"fmt"
	"strings"
)

// Machine is one sub-agent VM, whichever backend booted it.
type Machine interface {
	// Exec runs a command inside the guest, in the mounted workspace.
	Exec(ctx context.Context, args ...string) ([]byte, error)
	// Shutdown tears down a managed machine. No-op for attached machines.
	Shutdown() error
	// WorkspacePath translates a host path to its path in the guest.
	WorkspacePath(hostPath string) string
}

// Backend names a VM implementation.
type Backend string

const (
	// BackendStereOS boots stereOS mixtapes through the mb CLI. Default.
	BackendStereOS Backend = "stereos"
	// BackendSmol boots an OCI image as a smolvm microVM.
	BackendSmol Backend = "smol"
)

// Backends lists every supported backend, default first.
var Backends = []Backend{BackendStereOS, BackendSmol}

// ParseBackend resolves a user-supplied backend name. Empty means the
// default (stereOS). "smolvm" is accepted as an alias for "smol".
func ParseBackend(s string) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "stereos":
		return BackendStereOS, nil
	case "smol", "smolvm":
		return BackendSmol, nil
	}
	return "", fmt.Errorf("unknown VM backend %q (available: %s, %s)", s, BackendStereOS, BackendSmol)
}

// Binary is the host CLI the backend shells out to.
func (b Backend) Binary() string {
	if b == BackendSmol {
		return "smolvm"
	}
	return "mb"
}

// Options configures New.
type Options struct {
	Backend Backend
	// Name attaches to an existing, user-managed machine when set.
	// Otherwise a fresh managed machine is booted.
	Name    string
	HostDir string // host project dir, mounted at /workspace
	// JcardDir is where stereOS writes its ephemeral jcard. stereOS only.
	JcardDir string
	// Image is the OCI image to boot. smol only; empty uses DefaultSmolImage.
	Image string
}

// New boots or attaches a machine on the chosen backend. The bool reports
// whether sweeper owns the machine's lifecycle.
func New(opts Options) (Machine, bool, error) {
	return newMachine(opts, defaultRunner)
}

func newMachine(opts Options, runner cmdRunner) (Machine, bool, error) {
	switch opts.Backend {
	case BackendStereOS, "":
		if opts.Image != "" {
			return nil, false, fmt.Errorf("--vm-image applies to the smol backend only; stereOS boots a mixtape")
		}
		if opts.Name != "" {
			return &VM{Name: opts.Name, Dir: opts.HostDir, runner: runner}, false, nil
		}
		m, err := boot(NewVMName(), opts.HostDir, opts.JcardDir, runner)
		if err != nil {
			return nil, false, err
		}
		return m, true, nil
	case BackendSmol:
		if opts.Name != "" {
			return &SmolVM{Name: opts.Name, Dir: opts.HostDir, runner: runner}, false, nil
		}
		m, err := bootSmol(NewVMName(), opts.HostDir, opts.Image, runner)
		if err != nil {
			return nil, false, err
		}
		return m, true, nil
	}
	return nil, false, fmt.Errorf("unknown VM backend %q", opts.Backend)
}

// Name reports the machine's name on either backend.
func Name(m Machine) string {
	switch v := m.(type) {
	case *VM:
		return v.Name
	case *SmolVM:
		return v.Name
	}
	return ""
}
