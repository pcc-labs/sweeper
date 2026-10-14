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
	// activity. The sweep itself is bounded by --max-rounds; this is the
	// backstop behind that.
	daytonaTTLMinutes = 240
)

// daytonaInit mirrors smolInit: a custom snapshot may lack claude.
const daytonaInit = "command -v claude >/dev/null || npm install -g --silent @anthropic-ai/claude-code"

// daytonaGit is the identity for the in-sandbox baseline and per-task
// commits. Those commits exist so a later task's patch is only its own
// change; they never leave the sandbox.
const daytonaGit = "git -c user.name=sweeper -c user.email=sweeper@localhost"

// sandboxAPI is the slice of a Daytona sandbox sweeper drives. sdkSandbox
// satisfies it with the real SDK; tests substitute a fake.
type sandboxAPI interface {
	ID() string
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
	v := &DaytonaSandbox{Name: sb.ID(), Dir: hostDir, Managed: false, sb: sb, host: host}
	if err := v.populate(ctx, initCmd); err != nil {
		return nil, err
	}
	return v, nil
}

// daytonaUnpack lays the uploaded archive out as the workspace and commits
// it as the baseline every SyncBack diffs against. The workspace is cleared
// first so an attached sandbox does not keep files from a previous sweep.
var daytonaUnpack = strings.Join([]string{
	"set -e",
	"mkdir -p " + DaytonaWorkspace + " 2>/dev/null || { sudo mkdir -p " + DaytonaWorkspace + " && sudo chown \"$(id -u):$(id -g)\" " + DaytonaWorkspace + "; }",
	"find " + DaytonaWorkspace + " -mindepth 1 -delete",
	"tar -xzf " + daytonaArchive + " -C " + DaytonaWorkspace,
	"rm -f " + daytonaArchive,
	"cd " + DaytonaWorkspace,
	"git init -q",
	"git add -A",
	daytonaGit + " commit -q --allow-empty -m 'sweeper: workspace baseline'",
}, "; ")

func (v *DaytonaSandbox) populate(ctx context.Context, initCmd string) error {
	archive, err := packWorkspace(ctx, v.Dir, v.host)
	if err != nil {
		return fmt.Errorf("packing workspace: %w", err)
	}
	if err := v.sb.Upload(ctx, archive, daytonaArchive); err != nil {
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

// run executes a shell command in the sandbox and folds a non-zero exit into
// the error, with the output attached, matching CombinedOutput semantics.
func (v *DaytonaSandbox) run(ctx context.Context, command, cwd string) (string, error) {
	code, out, err := v.sb.Exec(ctx, command, cwd)
	if err != nil {
		return out, fmt.Errorf("daytona exec: %w\n%s", err, out)
	}
	if code != 0 {
		return out, fmt.Errorf("exit status %d\n%s", code, out)
	}
	return out, nil
}

// packWorkspace tars the tracked and untracked-but-not-ignored files of dir,
// as git sees them. .git itself stays home; the sandbox gets its own baseline
// commit instead, so node_modules, build output and history never upload.
func packWorkspace(ctx context.Context, dir string, host hostRunner) (io.Reader, error) {
	var buf bytes.Buffer
	if err := packWorkspaceTo(ctx, &buf, dir, host); err != nil {
		return nil, err
	}
	return &buf, nil
}

func packWorkspaceTo(ctx context.Context, w io.Writer, dir string, host hostRunner) error {
	out, err := host(ctx, dir, nil, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return fmt.Errorf("git ls-files in %s: %w\n%s", dir, err, out)
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
	out, err := v.run(ctx, shellJoin(args), DaytonaWorkspace)
	return []byte(out), err
}

// SyncBack pulls the sandbox's changes to one file (a path relative to the
// target dir, or absolute under it) onto the host, then commits them in the
// sandbox so the next sync of that file carries only new work. An empty
// hostPath syncs the whole workspace. A change the host cannot apply is an
// error: the task is then reported failed rather than silently lost.
func (v *DaytonaSandbox) SyncBack(ctx context.Context, hostPath string) error {
	spec, err := v.pathspec(hostPath)
	if err != nil {
		return err
	}
	v.syncMu.Lock()
	defer v.syncMu.Unlock()
	q := shellQuote(spec)
	diff, err := v.run(ctx, "git add -A -- "+q+" && git diff --cached --binary -- "+q, DaytonaWorkspace)
	if err != nil {
		return fmt.Errorf("collecting sandbox changes for %s: %w", spec, err)
	}
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	if out, err := v.host(ctx, v.Dir, []byte(diff), "git", "apply", "--whitespace=nowarn"); err != nil {
		return fmt.Errorf("applying sandbox changes for %s on host: %w\n%s", spec, err, out)
	}
	if _, err := v.run(ctx, daytonaGit+" commit -q -m "+shellQuote("sweeper: "+spec)+" -- "+q, DaytonaWorkspace); err != nil {
		return fmt.Errorf("recording synced changes for %s: %w", spec, err)
	}
	return nil
}

func (v *DaytonaSandbox) pathspec(hostPath string) (string, error) {
	if hostPath == "" {
		return ".", nil
	}
	rel := hostPath
	if filepath.IsAbs(hostPath) {
		r, err := filepath.Rel(v.Dir, hostPath)
		if err != nil {
			return "", err
		}
		rel = r
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the target dir %s", hostPath, v.Dir)
	}
	return filepath.ToSlash(rel), nil
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
