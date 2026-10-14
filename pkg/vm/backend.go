package vm

import (
	"context"
	"fmt"
	"os/exec"
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
	// BackendDaytona creates a Daytona cloud sandbox through the Daytona API.
	BackendDaytona Backend = "daytona"
)

// Backends lists every supported backend, default first.
var Backends = []Backend{BackendStereOS, BackendSmol, BackendDaytona}

// ParseBackend resolves a user-supplied backend name. Empty means the
// default (stereOS). "smolvm" is accepted as an alias for "smol".
func ParseBackend(s string) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "stereos":
		return BackendStereOS, nil
	case "smol", "smolvm":
		return BackendSmol, nil
	case "daytona":
		return BackendDaytona, nil
	}
	names := make([]string, len(Backends))
	for i, b := range Backends {
		names[i] = string(b)
	}
	return "", fmt.Errorf("unknown VM backend %q (available: %s)", s, strings.Join(names, ", "))
}

// Binary is the host CLI the backend shells out to. Empty for a backend
// that talks to an API instead.
func (b Backend) Binary() string {
	switch b {
	case BackendSmol:
		return "smolvm"
	case BackendDaytona:
		return ""
	}
	return "mb"
}

// CheckHost fails fast when the host cannot drive the backend: a CLI
// backend needs its binary on PATH, Daytona needs API credentials.
func (b Backend) CheckHost() error {
	if b == BackendDaytona {
		if !daytonaCredentialsPresent() {
			return fmt.Errorf("VM backend %q requires DAYTONA_API_KEY (or DAYTONA_JWT_TOKEN with DAYTONA_ORGANIZATION_ID) in the environment", b)
		}
		return nil
	}
	if _, err := exec.LookPath(b.Binary()); err != nil {
		return fmt.Errorf("VM backend %q requires the %q CLI on PATH", b, b.Binary())
	}
	return nil
}

// Options configures New.
type Options struct {
	Backend Backend
	// Name attaches to an existing, user-managed machine when set.
	// Otherwise a fresh managed machine is booted.
	Name    string
	HostDir string // host project dir, mounted (or uploaded) at /workspace
	// JcardDir is where stereOS writes its ephemeral jcard. stereOS only.
	JcardDir string
	// Image is what the guest boots from: an OCI image on smol (empty uses
	// DefaultSmolImage), a snapshot name on daytona (empty uses
	// DefaultDaytonaSnapshot). Rejected on stereOS.
	Image string
	// Init is a shell command run in the workspace after boot, before any
	// task dispatches, e.g. "npm ci". daytona only: the mount-based backends
	// see the host's installed dependencies already.
	Init string
}

// deps are the host-side seams New uses, swapped out by tests.
type deps struct {
	run     cmdRunner
	host    hostRunner
	daytona func() (sandboxClient, error)
}

func (d deps) withDefaults() deps {
	if d.run == nil {
		d.run = defaultRunner
	}
	if d.host == nil {
		d.host = defaultHostRunner
	}
	if d.daytona == nil {
		d.daytona = newSDKClient
	}
	return d
}

// New boots or attaches a machine on the chosen backend. The bool reports
// whether sweeper owns the machine's lifecycle.
func New(opts Options) (Machine, bool, error) {
	return newMachine(opts, deps{})
}

func newMachine(opts Options, d deps) (Machine, bool, error) {
	d = d.withDefaults()
	if opts.Init != "" && opts.Backend != BackendDaytona {
		return nil, false, fmt.Errorf("--vm-init applies to the daytona backend only; %s runs against the host's own dependencies", opts.Backend)
	}
	switch opts.Backend {
	case BackendStereOS, "":
		if opts.Image != "" {
			return nil, false, fmt.Errorf("--vm-image applies to the smol and daytona backends only; stereOS boots a mixtape")
		}
		if opts.Name != "" {
			return &VM{Name: opts.Name, Dir: opts.HostDir, runner: d.run}, false, nil
		}
		m, err := boot(NewVMName(), opts.HostDir, opts.JcardDir, d.run)
		if err != nil {
			return nil, false, err
		}
		return m, true, nil
	case BackendSmol:
		if opts.Name != "" {
			return &SmolVM{Name: opts.Name, Dir: opts.HostDir, runner: d.run}, false, nil
		}
		m, err := bootSmol(NewVMName(), opts.HostDir, opts.Image, d.run)
		if err != nil {
			return nil, false, err
		}
		return m, true, nil
	case BackendDaytona:
		client, err := d.daytona()
		if err != nil {
			return nil, false, err
		}
		if opts.Name != "" {
			m, err := attachDaytona(opts.Name, opts.HostDir, opts.Init, client, d.host)
			if err != nil {
				return nil, false, err
			}
			return m, false, nil
		}
		m, err := bootDaytona(NewVMName(), opts.HostDir, opts.Image, opts.Init, client, d.host)
		if err != nil {
			return nil, false, err
		}
		return m, true, nil
	}
	return nil, false, fmt.Errorf("unknown VM backend %q", opts.Backend)
}

// Name reports the machine's name on any backend.
func Name(m Machine) string {
	switch v := m.(type) {
	case *VM:
		return v.Name
	case *SmolVM:
		return v.Name
	case *DaytonaSandbox:
		return v.Name
	}
	return ""
}
