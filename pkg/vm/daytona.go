package vm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daytona/clients/sdk-go/pkg/types"
)

// DefaultDaytonaSnapshot boots when no --vm-image is given. Daytona's stock
// snapshots ship node and @anthropic-ai/claude-code, so nothing installs on
// boot. The target's own toolchain (node_modules, a linter) is not in it:
// bake a snapshot with `daytona snapshot create` and pass it as --vm-image,
// or install at boot with --vm-init.
const DefaultDaytonaSnapshot = "daytona-medium"

// DaytonaWorkspace is where the target dir is unpacked inside the sandbox.
// Nothing is mounted: Daytona runs remotely, so the tree is uploaded once at
// boot and each task's changes are pulled back as a patch (see SyncBack).
const DaytonaWorkspace = "/workspace"

const (
	daytonaArchive       = "/tmp/sweeper-workspace.tgz"
	daytonaCreateTimeout = 3 * time.Minute
	daytonaDeleteTimeout = 2 * time.Minute
	// daytonaAutoStopMinutes stops an idle sandbox server-side. Combined with
	// Ephemeral, a stopped sandbox is deleted, so a host that dies before
	// Shutdown runs still does not leak a billed sandbox.
	daytonaAutoStopMinutes = 60
	// daytonaTTLMinutes caps a sandbox's wall-clock life regardless of
	// activity: a backstop behind the idle stop, not a budget for the
	// sweep, so it is set well past any sweep --max-rounds should allow.
	daytonaTTLMinutes = 12 * 60
)

// daytonaInit is the same install step smol runs: a custom snapshot may
// lack claude.
const daytonaInit = smolInit

// daytonaGit is the identity for the in-sandbox baseline and per-task
// commits. Those commits exist so a later task's patch is only its own
// change; they never leave the sandbox.
const daytonaGit = "git -c user.name=sweeper -c user.email=sweeper@localhost"

// sandboxAPI is the slice of a Daytona sandbox sweeper drives. sdkSandbox
// satisfies it with the real SDK; tests substitute a fake.
type sandboxAPI interface {
	ID() string
	// Start brings a stopped sandbox up; a no-op when already running.
	Start(ctx context.Context) error
	// Exec runs command through the sandbox shell in cwd and returns its
	// exit code and combined output.
	Exec(ctx context.Context, command, cwd string) (exitCode int, output string, err error)
	Upload(ctx context.Context, src io.Reader, remotePath string) error
	Delete(ctx context.Context) error
}

// sandboxClient creates or looks up sandboxes.
type sandboxClient interface {
	Create(ctx context.Context, params types.SnapshotParams) (sandboxAPI, error)
	Get(ctx context.Context, nameOrID string) (sandboxAPI, error)
}

// hostRunner runs a command on the host in dir, feeding stdin when non-nil,
// and returns combined output.
type hostRunner func(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error)

func defaultHostRunner(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

// DaytonaSandbox is a Daytona cloud sandbox holding a copy of the target
// dir. Unlike the mount-based backends, it owns a remote copy of the tree:
// Exec runs inside that copy and SyncBack brings a file's changes home.
type DaytonaSandbox struct {
	Name     string // sandbox ID
	Dir      string // host project dir
	Snapshot string
	Managed  bool // true = sweeper owns create/delete
	sb       sandboxAPI
	host     hostRunner
	// syncMu serializes SyncBack: concurrent tasks finish at the same time,
	// and the sandbox's single git index cannot take parallel adds.
	syncMu sync.Mutex
}

// BootDaytona creates a managed sandbox from snapshot, uploads hostDir into
// it, and runs initCmd (if any) in the workspace.
func BootDaytona(name, hostDir, snapshot, initCmd string) (*DaytonaSandbox, error) {
	client, err := newSDKClient()
	if err != nil {
		return nil, err
	}
	return bootDaytona(name, hostDir, snapshot, initCmd, client, defaultHostRunner)
}

func bootDaytona(name, hostDir, snapshot, initCmd string, client sandboxClient, host hostRunner) (*DaytonaSandbox, error) {
	if snapshot == "" {
		snapshot = DefaultDaytonaSnapshot
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("the daytona VM backend needs ANTHROPIC_API_KEY on the host; it is injected into the sandbox environment")
	}
	ctx := context.Background()
	// Fail on an unpackable target (not a git checkout) before a sandbox
	// exists to be billed for it.
	if err := checkWorkspace(ctx, hostDir, host); err != nil {
		return nil, err
	}
	sb, err := client.Create(ctx, daytonaCreateParams(name, snapshot, key))
	if err != nil {
		return nil, fmt.Errorf("daytona sandbox create failed: %w", err)
	}
	v := &DaytonaSandbox{Name: sb.ID(), Dir: hostDir, Snapshot: snapshot, Managed: true, sb: sb, host: host}
	if err := v.populate(ctx, initCmd); err != nil {
		// Leave nothing behind: a created sandbox is billed until deleted.
		_ = sb.Delete(ctx)
		return nil, err
	}
	return v, nil
}

// daytonaCreateParams builds the create request. The API key travels as a
// sandbox env var; the host's ANTHROPIC_BASE_URL rides along when set, which
// is how a sweep points its sandboxes at a tapes proxy reachable from
// Daytona so the sessions outlive the sandbox.
func daytonaCreateParams(name, snapshot, key string) types.SnapshotParams {
	env := map[string]string{
		"ANTHROPIC_API_KEY": key,
		// claude refuses --dangerously-skip-permissions as root outside a
		// sandbox; harmless when the snapshot runs an unprivileged user.
		"IS_SANDBOX": "1",
	}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		env["ANTHROPIC_BASE_URL"] = base
	}
	autoStop, ttl := daytonaAutoStopMinutes, daytonaTTLMinutes
	return types.SnapshotParams{
		Snapshot: snapshot,
		SandboxBaseParams: types.SandboxBaseParams{
			Name:             name,
			EnvVars:          env,
			Labels:           map[string]string{"sweeper": name},
			AutoStopInterval: &autoStop,
			TtlMinutes:       &ttl,
			Ephemeral:        true,
		},
	}
}

// AttachDaytona reuses an existing sandbox by ID or name. The target dir is
// still uploaded, replacing whatever the workspace held, but the sandbox is
// left running on Shutdown.
func AttachDaytona(name, hostDir, initCmd string) (*DaytonaSandbox, error) {
	client, err := newSDKClient()
	if err != nil {
		return nil, err
	}
	return attachDaytona(name, hostDir, initCmd, client, defaultHostRunner)
}

func attachDaytona(name, hostDir, initCmd string, client sandboxClient, host hostRunner) (*DaytonaSandbox, error) {
	ctx := context.Background()
	sb, err := client.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("daytona sandbox %q: %w", name, err)
	}
	// An attached sandbox may have been auto-stopped since it was created.
	if err := sb.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting daytona sandbox %q: %w", name, err)
	}
	v := &DaytonaSandbox{Name: sb.ID(), Dir: hostDir, Managed: false, sb: sb, host: host}
	if err := v.populate(ctx, initCmd); err != nil {
		return nil, err
	}
	return v, nil
}

// daytonaUnpack lays the uploaded archive out as the workspace and commits
// it as the baseline every SyncBack diffs against. The add is forced so a
// host file that is tracked despite matching .gitignore stays in the
// baseline. The workspace is cleared first so an attached sandbox does not
// keep files from a previous sweep.
var daytonaUnpack = strings.Join([]string{
	"set -e",
	"mkdir -p " + DaytonaWorkspace + " 2>/dev/null || { sudo mkdir -p " + DaytonaWorkspace + " && sudo chown \"$(id -u):$(id -g)\" " + DaytonaWorkspace + "; }",
	"find " + DaytonaWorkspace + " -mindepth 1 -delete",
	"tar -xzf " + daytonaArchive + " -C " + DaytonaWorkspace,
	"rm -f " + daytonaArchive,
	"cd " + DaytonaWorkspace,
	"git init -q",
	"git add -A -f",
	daytonaGit + " commit -q --allow-empty -m 'sweeper: workspace baseline'",
}, "; ")

func (v *DaytonaSandbox) populate(ctx context.Context, initCmd string) error {
	archive := packWorkspace(ctx, v.Dir, v.host)
	err := v.sb.Upload(ctx, archive, daytonaArchive)
	// Closing the reader releases the packer if the upload stopped early.
	_ = archive.Close()
	if err != nil {
		return fmt.Errorf("uploading workspace: %w", err)
	}
	if _, err := v.run(ctx, daytonaUnpack, "/"); err != nil {
		return fmt.Errorf("unpacking workspace: %w", err)
	}
	if _, err := v.run(ctx, daytonaInit, DaytonaWorkspace); err != nil {
		return fmt.Errorf("installing claude: %w", err)
	}
	if initCmd != "" {
		if _, err := v.run(ctx, initCmd, DaytonaWorkspace); err != nil {
			return fmt.Errorf("running vm init %q: %w", initCmd, err)
		}
	}
	return nil
}

// exec runs a shell command in the sandbox and folds a non-zero exit into
// the error, matching CombinedOutput: the output comes back separately.
func (v *DaytonaSandbox) exec(ctx context.Context, command, cwd string) (string, error) {
	code, out, err := v.sb.Exec(ctx, command, cwd)
	if err != nil {
		return out, fmt.Errorf("daytona exec: %w", err)
	}
	if code != 0 {
		return out, fmt.Errorf("exit status %d", code)
	}
	return out, nil
}

// run is exec for sweeper's own setup and sync commands, whose output is
// otherwise discarded, so it is attached to the error.
func (v *DaytonaSandbox) run(ctx context.Context, command, cwd string) (string, error) {
	out, err := v.exec(ctx, command, cwd)
	if err != nil {
		return out, fmt.Errorf("%w\n%s", err, out)
	}
	return out, nil
}

// lsFiles lists the files git would archive: tracked plus untracked, minus
// ignored, NUL-separated.
func lsFiles(ctx context.Context, dir string, host hostRunner) ([]byte, error) {
	out, err := host(ctx, dir, nil, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w\n%s", dir, err, out)
	}
	return out, nil
}

// checkWorkspace verifies the target dir can be packed. The daytona backend
// needs the target to be a git checkout: that is what decides which files
// travel and what a patch applies to.
func checkWorkspace(ctx context.Context, dir string, host hostRunner) error {
	if _, err := lsFiles(ctx, dir, host); err != nil {
		return fmt.Errorf("the daytona backend needs the target dir to be a git checkout: %w", err)
	}
	return nil
}

// packWorkspace streams a gzipped tar of the tracked and untracked-but-not-
// ignored files of dir, as git sees them. .git itself stays home; the
// sandbox gets its own baseline commit instead, so node_modules, build
// output and history never upload. Packing errors surface from the reader,
// so the uploader reports them; closing the reader stops the packer.
func packWorkspace(ctx context.Context, dir string, host hostRunner) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(packWorkspaceTo(ctx, pw, dir, host))
	}()
	return pr
}

func packWorkspaceTo(ctx context.Context, w io.Writer, dir string, host hostRunner) error {
	out, err := lsFiles(ctx, dir, host)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	seen := make(map[string]bool)
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		if err := addToTar(tw, dir, rel); err != nil {
			return err
		}
	}
	err = tw.Close()
	if err == nil {
		err = gz.Close()
	}
	return err
}

func addToTar(tw *tar.Writer, dir, rel string) error {
	full := filepath.Join(dir, rel)
	fi, err := os.Lstat(full)
	if os.IsNotExist(err) {
		return nil // tracked but deleted on disk
	}
	var link string
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		link, err = os.Readlink(full)
	}
	if err != nil {
		return err
	}
	if link == "" && !fi.Mode().IsRegular() {
		return nil // submodule dir, socket, device
	}
	// FileInfoHeader only fails for modes filtered out above.
	hdr, _ := tar.FileInfoHeader(fi, link)
	hdr.Name = filepath.ToSlash(rel)
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if link != "" {
		return nil
	}
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}

// Exec runs a command in the sandbox workspace.
func (v *DaytonaSandbox) Exec(ctx context.Context, args ...string) ([]byte, error) {
	out, err := v.exec(ctx, shellJoin(args), DaytonaWorkspace)
	return []byte(out), err
}

// SyncBack pulls every change in the sandbox workspace onto the host and
// commits it in the sandbox as the new baseline. It is whole-tree, not
// per-file: an agent fixing one file routinely touches its callers too, and
// a change that only reached the sandbox would leave the two trees apart
// for the rest of the sweep. hostPath names the task that finished, for
// messages. A change the host cannot apply is an error: the task is then
// reported failed rather than silently lost.
func (v *DaytonaSandbox) SyncBack(ctx context.Context, hostPath string) error {
	label := hostPath
	if label == "" {
		label = "workspace"
	}
	v.syncMu.Lock()
	defer v.syncMu.Unlock()
	// Stage, diff the index, then commit the index rather than the working
	// tree: an agent still running may edit a staged file between the diff
	// and the commit, and that edit must stay pending for the next sync.
	diff, err := v.run(ctx, "git add -A -f && git diff --cached --binary", DaytonaWorkspace)
	if err != nil {
		return fmt.Errorf("collecting sandbox changes after %s: %w", label, err)
	}
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	if out, err := v.host(ctx, v.Dir, []byte(diff), "git", v.applyArgs(ctx)...); err != nil {
		return fmt.Errorf("applying sandbox changes after %s on host: %w\n%s", label, err, out)
	}
	if _, err := v.run(ctx, daytonaGit+" commit -q -m "+shellQuote("sweeper: after "+label), DaytonaWorkspace); err != nil {
		return fmt.Errorf("recording synced changes after %s: %w", label, err)
	}
	return nil
}

// applyArgs builds the host-side git apply. Patch paths are relative to the
// target dir, but when that dir sits inside a larger repository git apply
// resolves paths from the repository root, so the subdirectory prefix is
// prepended. Outside any repository git apply behaves like patch and the
// paths are already right.
func (v *DaytonaSandbox) applyArgs(ctx context.Context) []string {
	args := []string{"apply", "--whitespace=nowarn"}
	if out, err := v.host(ctx, v.Dir, nil, "git", "rev-parse", "--show-prefix"); err == nil {
		if prefix := strings.TrimSpace(string(out)); prefix != "" {
			args = append(args, "--directory="+strings.TrimSuffix(prefix, "/"))
		}
	}
	return args
}

// Shutdown deletes a managed sandbox. No-op for attached sandboxes.
func (v *DaytonaSandbox) Shutdown() error {
	if !v.Managed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), daytonaDeleteTimeout)
	defer cancel()
	if err := v.sb.Delete(ctx); err != nil {
		return fmt.Errorf("daytona sandbox delete failed: %w", err)
	}
	return nil
}

// WorkspacePath translates a host path to the sandbox workspace path.
func (v *DaytonaSandbox) WorkspacePath(hostPath string) string {
	rel, err := filepath.Rel(v.Dir, hostPath)
	if err != nil {
		return hostPath
	}
	return filepath.Join(DaytonaWorkspace, rel)
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// shellQuote makes s a single shell word.
func shellQuote(s string) string {
	if s != "" && shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellJoin renders argv as one command line for the sandbox shell. The
// prompt is one argument, so its quotes and newlines survive intact.
func shellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}
