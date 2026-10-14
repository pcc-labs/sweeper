package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultSmolImage boots when no --vm-image is given. smolInit installs the
// claude CLI on first start, since a stock image does not carry it. Pass an
// image with claude preinstalled to skip that install on every run.
const DefaultSmolImage = "node:22-bookworm-slim"

const smolInit = "command -v claude >/dev/null || npm install -g --silent @anthropic-ai/claude-code"

// smolCredential binds ANTHROPIC_API_KEY without putting it in the guest.
// The guest sees a placeholder; smolvm swaps in the host's real value only
// on HTTPS requests to api.anthropic.com.
const smolCredential = "anthropic=ANTHROPIC_API_KEY@api.anthropic.com"

// SmolVM is a smolvm microVM.
type SmolVM struct {
	Name    string
	Dir     string // host project dir
	Image   string
	Managed bool // true = sweeper owns boot/teardown
	runner  cmdRunner
}

// BootSmol creates and starts a managed smolvm machine with hostDir mounted
// read-write at /workspace.
func BootSmol(name, hostDir, image string) (*SmolVM, error) {
	return bootSmol(name, hostDir, image, defaultRunner)
}

func bootSmol(name, hostDir, image string, runner cmdRunner) (*SmolVM, error) {
	if image == "" {
		image = DefaultSmolImage
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return nil, fmt.Errorf("the smol VM backend needs ANTHROPIC_API_KEY on the host; smolvm substitutes it into guest HTTPS requests")
	}
	ctx := context.Background()
	out, err := runner(ctx, "smolvm", smolCreateArgs(name, hostDir, image)...)
	if err != nil {
		return nil, fmt.Errorf("smolvm machine create failed: %w\n%s", err, out)
	}
	v := &SmolVM{Name: name, Dir: hostDir, Image: image, Managed: true, runner: runner}
	out, err = runner(ctx, "smolvm", "machine", "start", "--name", name)
	if err != nil {
		// Leave nothing behind: a created-but-unstarted machine still
		// holds a record and disks.
		_, _ = runner(ctx, "smolvm", "machine", "delete", "--name", name, "--force")
		return nil, fmt.Errorf("smolvm machine start failed: %w\n%s", err, out)
	}
	return v, nil
}

func smolCreateArgs(name, hostDir, image string) []string {
	return []string{
		"machine", "create",
		"--name", name,
		"--image", image,
		"-v", hostDir + ":/workspace",
		"-w", "/workspace",
		// The guest runs as root; claude refuses
		// --dangerously-skip-permissions as root outside a sandbox.
		"-e", "IS_SANDBOX=1",
		"--credential", smolCredential,
		"--init", smolInit,
	}
}

// AttachSmol returns a handle for an existing, user-managed smolvm machine.
func AttachSmol(name, hostDir string) *SmolVM {
	return &SmolVM{Name: name, Dir: hostDir, Managed: false, runner: defaultRunner}
}

// Exec runs a command in the machine's workspace via smolvm machine exec.
func (v *SmolVM) Exec(ctx context.Context, args ...string) ([]byte, error) {
	smolArgs := []string{"machine", "exec", "--name", v.Name, "-w", "/workspace", "--"}
	smolArgs = append(smolArgs, args...)
	return v.runner(ctx, "smolvm", smolArgs...)
}

// Shutdown stops and deletes a managed machine. No-op for attached machines.
// Stop is graceful, so any staged mounts drain to the host first.
func (v *SmolVM) Shutdown() error {
	if !v.Managed {
		return nil
	}
	ctx := context.Background()
	if out, err := v.runner(ctx, "smolvm", "machine", "stop", "--name", v.Name); err != nil {
		return fmt.Errorf("smolvm machine stop failed: %w\n%s", err, out)
	}
	if out, err := v.runner(ctx, "smolvm", "machine", "delete", "--name", v.Name, "--force"); err != nil {
		return fmt.Errorf("smolvm machine delete failed: %w\n%s", err, out)
	}
	return nil
}

// WorkspacePath translates a host path to the guest workspace path.
func (v *SmolVM) WorkspacePath(hostPath string) string {
	rel, err := filepath.Rel(v.Dir, hostPath)
	if err != nil {
		return hostPath
	}
	return filepath.Join("/workspace", rel)
}
