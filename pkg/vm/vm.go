package vm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
)

type cmdRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// VM represents a stereOS virtual machine managed by mb.
type VM struct {
	Name      string
	Dir       string // host project dir
	Managed   bool   // true = sweeper owns boot/teardown
	runner    cmdRunner
	jcardPath string
}

// NewVMName generates a unique sweeper VM name.
func NewVMName() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("sweeper-%s", hex.EncodeToString(b))
}

// Boot creates and starts a new managed stereOS VM.
func Boot(name, hostDir, jcardDir string) (*VM, error) {
	return boot(name, hostDir, jcardDir, defaultRunner)
}

func boot(name, hostDir, jcardDir string, runner cmdRunner) (*VM, error) {
	jcardPath, err := GenerateJcard(jcardDir, name, hostDir)
	if err != nil {
		return nil, err
	}
	out, err := runner(context.Background(), "mb", "up", "--config", jcardPath)
	if err != nil {
		CleanupJcard(jcardPath)
		return nil, fmt.Errorf("mb up failed: %w\n%s", err, out)
	}
	return &VM{
		Name:      name,
		Dir:       hostDir,
		Managed:   true,
		runner:    runner,
		jcardPath: jcardPath,
	}, nil
}

// Attach returns a VM handle for an existing (user-managed) VM.
func Attach(name, hostDir string) *VM {
	return &VM{
		Name:    name,
		Dir:     hostDir,
		Managed: false,
		runner:  defaultRunner,
	}
}

// Exec runs a command inside the VM via mb ssh and returns its output.
func (v *VM) Exec(ctx context.Context, args ...string) ([]byte, error) {
	mbArgs := []string{"ssh", "--user", "agent", v.Name, "--"}
	mbArgs = append(mbArgs, args...)
	return v.runner(ctx, "mb", mbArgs...)
}

// Shutdown tears down a managed VM. No-op for unmanaged VMs.
func (v *VM) Shutdown() error {
	if !v.Managed {
		return nil
	}
	out, err := v.runner(context.Background(), "mb", "destroy", v.Name, "--yes")
	if err != nil {
		return fmt.Errorf("mb destroy failed: %w\n%s", err, out)
	}
	if v.jcardPath != "" {
		CleanupJcard(v.jcardPath)
	}
	return nil
}

// WorkspacePath translates a host path to the VM workspace path.
func (v *VM) WorkspacePath(hostPath string) string {
	rel, err := filepath.Rel(v.Dir, hostPath)
	if err != nil {
		return hostPath
	}
	return filepath.Join("/workspace", rel)
}
