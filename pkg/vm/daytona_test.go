package vm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daytona/clients/sdk-go/pkg/types"
)

// fakeSandbox records what sweeper asks of a sandbox and answers execs
// through respond, which defaults to success with empty output.
type fakeSandbox struct {
	id        string
	execs     []fakeExec
	uploads   map[string][]byte
	deleted   bool
	respond   func(command string) (int, string, error)
	uploadOK  bool
	deleteErr error
}

type fakeExec struct{ command, cwd string }

func newFakeSandbox(id string) *fakeSandbox {
	return &fakeSandbox{id: id, uploads: map[string][]byte{}, uploadOK: true}
}

func (f *fakeSandbox) ID() string { return f.id }
func (f *fakeSandbox) Exec(ctx context.Context, command, cwd string) (int, string, error) {
	f.execs = append(f.execs, fakeExec{command, cwd})
	if f.respond != nil {
		return f.respond(command)
	}
	return 0, "", nil
}
func (f *fakeSandbox) Upload(ctx context.Context, src io.Reader, remotePath string) error {
	if !f.uploadOK {
		return fmt.Errorf("upload refused")
	}
	b, _ := io.ReadAll(src)
	f.uploads[remotePath] = b
	return nil
}
func (f *fakeSandbox) Delete(ctx context.Context) error { f.deleted = true; return f.deleteErr }

type fakeClient struct {
	sb        *fakeSandbox
	created   []types.SnapshotParams
	got       []string
	createErr error
	getErr    error
}

func (c *fakeClient) Create(ctx context.Context, p types.SnapshotParams) (sandboxAPI, error) {
	c.created = append(c.created, p)
	if c.createErr != nil {
		return nil, c.createErr
	}
	return c.sb, nil
}
func (c *fakeClient) Get(ctx context.Context, nameOrID string) (sandboxAPI, error) {
	c.got = append(c.got, nameOrID)
	if c.getErr != nil {
		return nil, c.getErr
	}
	c.sb.id = nameOrID
	return c.sb, nil
}

// fakeDaytonaFactory hands newMachine a client whose sandbox accepts
// everything, for tests that exercise routing rather than the backend.
func fakeDaytonaFactory(t *testing.T) func() (sandboxClient, error) {
	return func() (sandboxClient, error) {
		return &fakeClient{sb: newFakeSandbox("sweeper-fake")}, nil
	}
}

func failingDaytonaFactory() (sandboxClient, error) {
	return &fakeClient{sb: newFakeSandbox("x"), createErr: fmt.Errorf("quota")}, nil
}

// emptyHost is a host whose target dir has no files and accepts any patch.
func emptyHost(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
	return nil, nil
}

type hostCall struct {
	dir   string
	stdin []byte
	args  []string
}

func recordingHost(calls *[]hostCall, lsFiles string, applyErr error) hostRunner {
	return func(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, hostCall{dir, stdin, append([]string{name}, args...)})
		if len(args) > 0 && args[0] == "ls-files" {
			return []byte(lsFiles), nil
		}
		if len(args) > 0 && args[0] == "apply" && applyErr != nil {
			return []byte("error: patch failed"), applyErr
		}
		return nil, nil
	}
}

func TestBootDaytonaCreatesUploadsAndInits(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	sb := newFakeSandbox("sb-123")
	client := &fakeClient{sb: sb}
	v, err := bootDaytona("sweeper-t", "/host/proj", "", "npm ci", client, emptyHost)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Managed || v.Name != "sb-123" || v.Snapshot != DefaultDaytonaSnapshot {
		t.Errorf("got managed=%v name=%q snapshot=%q", v.Managed, v.Name, v.Snapshot)
	}
	if len(client.created) != 1 {
		t.Fatalf("expected one create, got %d", len(client.created))
	}
	p := client.created[0]
	if p.Snapshot != DefaultDaytonaSnapshot || p.Name != "sweeper-t" || !p.Ephemeral {
		t.Errorf("create params: snapshot=%q name=%q ephemeral=%v", p.Snapshot, p.Name, p.Ephemeral)
	}
	if p.EnvVars["ANTHROPIC_API_KEY"] != "sk-test" || p.EnvVars["IS_SANDBOX"] != "1" {
		t.Errorf("sandbox env should carry the key and IS_SANDBOX, got %v", p.EnvVars)
	}
	if _, ok := p.EnvVars["ANTHROPIC_BASE_URL"]; ok {
		t.Error("ANTHROPIC_BASE_URL must not be set when the host has none")
	}
	if p.AutoStopInterval == nil || *p.AutoStopInterval != daytonaAutoStopMinutes || p.TtlMinutes == nil || *p.TtlMinutes != daytonaTTLMinutes {
		t.Errorf("expected auto-stop and TTL backstops, got %v / %v", p.AutoStopInterval, p.TtlMinutes)
	}
	if p.Labels["sweeper"] != "sweeper-t" {
		t.Errorf("expected a sweeper label, got %v", p.Labels)
	}
	if _, ok := sb.uploads[daytonaArchive]; !ok {
		t.Errorf("workspace archive should be uploaded to %s, got %v", daytonaArchive, sb.uploads)
	}
	if len(sb.execs) != 3 {
		t.Fatalf("expected unpack, claude install, user init; got %v", sb.execs)
	}
	if !strings.Contains(sb.execs[0].command, "tar -xzf "+daytonaArchive) || !strings.Contains(sb.execs[0].command, "git init") {
		t.Errorf("first exec should unpack and baseline the workspace, got %q", sb.execs[0].command)
	}
	if sb.execs[1].command != daytonaInit || sb.execs[1].cwd != DaytonaWorkspace {
		t.Errorf("second exec should install claude in the workspace, got %v", sb.execs[1])
	}
	if sb.execs[2].command != "npm ci" || sb.execs[2].cwd != DaytonaWorkspace {
		t.Errorf("third exec should be the user init in the workspace, got %v", sb.execs[2])
	}
	if sb.deleted {
		t.Error("a healthy boot must not delete the sandbox")
	}
}

func TestBootDaytonaForwardsBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", "http://tapes.internal:8091")
	client := &fakeClient{sb: newFakeSandbox("sb")}
	if _, err := bootDaytona("n", "/h", "", "", client, emptyHost); err != nil {
		t.Fatal(err)
	}
	if got := client.created[0].EnvVars["ANTHROPIC_BASE_URL"]; got != "http://tapes.internal:8091" {
		t.Errorf("host ANTHROPIC_BASE_URL should reach the sandbox, got %q", got)
	}
}

func TestBootDaytonaRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	client := &fakeClient{sb: newFakeSandbox("sb")}
	if _, err := bootDaytona("n", "/h", "", "", client, emptyHost); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("expected an API key error, got %v", err)
	}
	if len(client.created) != 0 {
		t.Error("no sandbox should be created without a key")
	}
}

func TestBootDaytonaDeletesOnSetupFailure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) {
		if strings.Contains(command, "tar -xzf") {
			return 1, "tar: short read", nil
		}
		return 0, "", nil
	}
	v, err := bootDaytona("n", "/h", "custom-snap", "", &fakeClient{sb: sb}, emptyHost)
	if err == nil || v != nil {
		t.Fatalf("expected a boot error, got v=%v err=%v", v, err)
	}
	if !strings.Contains(err.Error(), "tar: short read") {
		t.Errorf("error should carry the sandbox output, got %v", err)
	}
	if !sb.deleted {
		t.Error("a sandbox that failed setup must be deleted, not leaked")
	}
}

func TestBootDaytonaUploadFailureDeletes(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	sb := newFakeSandbox("sb")
	sb.uploadOK = false
	if _, err := bootDaytona("n", "/h", "", "", &fakeClient{sb: sb}, emptyHost); err == nil {
		t.Fatal("expected upload failure")
	}
	if !sb.deleted {
		t.Error("sandbox should be deleted after a failed upload")
	}
}

func TestAttachDaytonaReusesSandbox(t *testing.T) {
	sb := newFakeSandbox("")
	client := &fakeClient{sb: sb}
	v, err := attachDaytona("warm-1", "/h", "", client, emptyHost)
	if err != nil {
		t.Fatal(err)
	}
	if v.Managed || v.Name != "warm-1" || len(client.created) != 0 || len(client.got) != 1 {
		t.Errorf("attach should look up, not create: managed=%v name=%q created=%d got=%v", v.Managed, v.Name, len(client.created), client.got)
	}
	if _, ok := sb.uploads[daytonaArchive]; !ok {
		t.Error("attach should still upload the target dir")
	}
	if !strings.Contains(sb.execs[0].command, "find "+DaytonaWorkspace+" -mindepth 1 -delete") {
		t.Errorf("attach should clear a previous workspace first, got %q", sb.execs[0].command)
	}
	if err := v.Shutdown(); err != nil || sb.deleted {
		t.Errorf("attached sandbox must survive Shutdown: err=%v deleted=%v", err, sb.deleted)
	}
}

func TestDaytonaExecQuotesArgsAndReportsExit(t *testing.T) {
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) { return 2, "lint failed", nil }
	v := &DaytonaSandbox{Name: "sb", Dir: "/h", sb: sb, host: emptyHost}
	out, err := v.Exec(context.Background(), "claude", "--print", "fix it's \"here\"\nnow")
	if err == nil || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("expected exit status error, got %v", err)
	}
	if string(out) != "lint failed" {
		t.Errorf("output should be returned alongside the error, got %q", out)
	}
	want := `claude --print 'fix it'\''s "here"` + "\nnow'"
	if got := sb.execs[0]; got.command != want || got.cwd != DaytonaWorkspace {
		t.Errorf("exec = %+v, want command %q in %s", got, want, DaytonaWorkspace)
	}
}

func TestDaytonaSyncBackAppliesAndCommits(t *testing.T) {
	const patch = "diff --git a/src/a.go b/src/a.go\n--- a/src/a.go\n+++ b/src/a.go\n@@ -1 +1 @@\n-x\n+y\n"
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) {
		if strings.Contains(command, "git diff --cached") {
			return 0, patch, nil
		}
		return 0, "", nil
	}
	var calls []hostCall
	v := &DaytonaSandbox{Name: "sb", Dir: "/host/proj", sb: sb, host: recordingHost(&calls, "", nil)}
	if err := v.SyncBack(context.Background(), "src/a.go"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].dir != "/host/proj" || strings.Join(calls[0].args, " ") != "git apply --whitespace=nowarn" || string(calls[0].stdin) != patch {
		t.Errorf("expected the diff piped into git apply in the target dir, got %+v", calls)
	}
	if len(sb.execs) != 2 {
		t.Fatalf("expected collect then commit, got %v", sb.execs)
	}
	if !strings.Contains(sb.execs[0].command, "git add -A -- src/a.go && git diff --cached --binary -- src/a.go") {
		t.Errorf("collect command = %q", sb.execs[0].command)
	}
	if !strings.HasPrefix(sb.execs[1].command, daytonaGit+" commit") || !strings.HasSuffix(sb.execs[1].command, "-- src/a.go") {
		t.Errorf("commit should be scoped to the synced file, got %q", sb.execs[1].command)
	}
}

func TestDaytonaSyncBackNoChangesIsNoop(t *testing.T) {
	sb := newFakeSandbox("sb")
	var calls []hostCall
	v := &DaytonaSandbox{Name: "sb", Dir: "/h", sb: sb, host: recordingHost(&calls, "", nil)}
	if err := v.SyncBack(context.Background(), "a.go"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || len(sb.execs) != 1 {
		t.Errorf("an empty diff should neither apply nor commit: host=%v execs=%v", calls, sb.execs)
	}
}

func TestDaytonaSyncBackHostApplyFailure(t *testing.T) {
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) {
		if strings.Contains(command, "git diff --cached") {
			return 0, "diff --git a/a b/a\n", nil
		}
		return 0, "", nil
	}
	var calls []hostCall
	v := &DaytonaSandbox{Name: "sb", Dir: "/h", sb: sb, host: recordingHost(&calls, "", fmt.Errorf("exit 1"))}
	err := v.SyncBack(context.Background(), "a")
	if err == nil || !strings.Contains(err.Error(), "patch failed") {
		t.Fatalf("expected the host's apply error, got %v", err)
	}
	if len(sb.execs) != 1 {
		t.Error("a change the host rejected must not be committed in the sandbox")
	}
}

func TestDaytonaSyncBackPathspec(t *testing.T) {
	v := &DaytonaSandbox{Dir: "/host/proj"}
	cases := map[string]string{"": ".", "src/a.go": "src/a.go", "/host/proj/src/b.go": "src/b.go", "./c.go": "c.go"}
	for in, want := range cases {
		got, err := v.pathspec(in)
		if err != nil || got != want {
			t.Errorf("pathspec(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"../etc/passwd", "/host/other/x"} {
		if _, err := v.pathspec(in); err == nil {
			t.Errorf("pathspec(%q) should be rejected", in)
		}
	}
}

func TestDaytonaShutdownDeletesManagedOnly(t *testing.T) {
	sb := newFakeSandbox("sb")
	v := &DaytonaSandbox{Name: "sb", Managed: true, sb: sb}
	if err := v.Shutdown(); err != nil || !sb.deleted {
		t.Errorf("managed shutdown should delete: err=%v deleted=%v", err, sb.deleted)
	}
	sb2 := newFakeSandbox("sb2")
	u := &DaytonaSandbox{Name: "sb2", sb: sb2}
	if err := u.Shutdown(); err != nil || sb2.deleted {
		t.Errorf("unmanaged shutdown must not delete: err=%v deleted=%v", err, sb2.deleted)
	}
}

func TestDaytonaWorkspacePath(t *testing.T) {
	v := &DaytonaSandbox{Dir: "/host/proj"}
	if got := v.WorkspacePath("/host/proj/src/a.go"); got != "/workspace/src/a.go" {
		t.Errorf("got %q", got)
	}
}

func TestPackWorkspaceFollowsGitListing(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("src/a.go", "package a\n")
	mustWrite("README.md", "hi\n")
	mustWrite("node_modules/x/index.js", "ignored\n")
	if err := os.Symlink("README.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	// deleted.go is tracked but gone from disk; node_modules is not listed.
	listing := strings.Join([]string{"src/a.go", "README.md", "link.md", "deleted.go", "src/a.go"}, "\x00") + "\x00"
	var calls []hostCall
	r, err := packWorkspace(context.Background(), dir, recordingHost(&calls, listing, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls[0].args, " ") != "git ls-files -z --cached --others --exclude-standard" || calls[0].dir != dir {
		t.Errorf("expected git ls-files in the target dir, got %+v", calls[0])
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		io.Copy(&b, tr)
		if h.Typeflag == tar.TypeSymlink {
			got[h.Name] = "-> " + h.Linkname
		} else {
			got[h.Name] = b.String()
		}
	}
	want := map[string]string{"src/a.go": "package a\n", "README.md": "hi\n", "link.md": "-> README.md"}
	if len(got) != len(want) {
		t.Fatalf("archive entries = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":          "plain",
		"src/a.go":       "src/a.go",
		"--model=x":      "--model=x",
		"":               "''",
		"has space":      "'has space'",
		"it's":           `'it'\''s'`,
		"a\nb":           "'a\nb'",
		`say "hi" $HOME`: `'say "hi" $HOME'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBootDaytonaFailsWhenPackingFails(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	sb := newFakeSandbox("sb")
	failingLs := func(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
		return []byte("fatal: not a git repository"), fmt.Errorf("exit 128")
	}
	_, err := bootDaytona("n", "/h", "", "", &fakeClient{sb: sb}, failingLs)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected the git error, got %v", err)
	}
	if !sb.deleted {
		t.Error("sandbox should be deleted when the workspace cannot be packed")
	}
}

func TestBootDaytonaUserInitFailure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) {
		if command == "npm ci" {
			return 1, "npm ERR! missing lockfile", nil
		}
		return 0, "", nil
	}
	_, err := bootDaytona("n", "/h", "", "npm ci", &fakeClient{sb: sb}, emptyHost)
	if err == nil || !strings.Contains(err.Error(), "npm ci") || !strings.Contains(err.Error(), "missing lockfile") {
		t.Fatalf("expected the init command and its output in the error, got %v", err)
	}
	if !sb.deleted {
		t.Error("sandbox should be deleted when init fails")
	}
}

func TestBootDaytonaCreateFailure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	_, err := bootDaytona("n", "/h", "", "", &fakeClient{sb: newFakeSandbox("sb"), createErr: fmt.Errorf("quota exceeded")}, emptyHost)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected the create error, got %v", err)
	}
}

func TestAttachDaytonaErrors(t *testing.T) {
	if _, err := attachDaytona("gone", "/h", "", &fakeClient{sb: newFakeSandbox(""), getErr: fmt.Errorf("404")}, emptyHost); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("expected a lookup error naming the sandbox, got %v", err)
	}
	sb := newFakeSandbox("")
	sb.uploadOK = false
	if _, err := attachDaytona("warm", "/h", "", &fakeClient{sb: sb}, emptyHost); err == nil {
		t.Error("expected an upload error on attach")
	}
	if sb.deleted {
		t.Error("an attached sandbox is never deleted, even when setup fails")
	}
}

func TestDaytonaExecTransportError(t *testing.T) {
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) { return 0, "partial", fmt.Errorf("connection reset") }
	v := &DaytonaSandbox{Name: "sb", Dir: "/h", sb: sb, host: emptyHost}
	out, err := v.Exec(context.Background(), "claude", "--print", "fix")
	if err == nil || !strings.Contains(err.Error(), "connection reset") || string(out) != "partial" {
		t.Errorf("expected transport error with output, got out=%q err=%v", out, err)
	}
}

func TestDaytonaSyncBackErrors(t *testing.T) {
	// Path outside the target dir.
	v := &DaytonaSandbox{Dir: "/host/proj", sb: newFakeSandbox("sb"), host: emptyHost}
	if err := v.SyncBack(context.Background(), "../escape.go"); err == nil {
		t.Error("expected a pathspec error")
	}
	// Collecting the diff fails.
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) { return 128, "fatal: bad index", nil }
	v = &DaytonaSandbox{Dir: "/h", sb: sb, host: emptyHost}
	if err := v.SyncBack(context.Background(), "a.go"); err == nil || !strings.Contains(err.Error(), "collecting") {
		t.Errorf("expected a collect error, got %v", err)
	}
	// Host applies but the sandbox commit fails.
	sb = newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) {
		switch {
		case strings.Contains(command, "git diff --cached"):
			return 0, "diff --git a/a.go b/a.go\n", nil
		case strings.Contains(command, " commit "):
			return 1, "nothing to commit", nil
		}
		return 0, "", nil
	}
	v = &DaytonaSandbox{Dir: "/h", sb: sb, host: emptyHost}
	if err := v.SyncBack(context.Background(), "a.go"); err == nil || !strings.Contains(err.Error(), "recording") {
		t.Errorf("expected a commit error, got %v", err)
	}
}

func TestDaytonaRelativeDirPaths(t *testing.T) {
	// A relative Dir cannot be related to an absolute path; both helpers
	// fall back rather than guess.
	v := &DaytonaSandbox{Dir: "rel/proj"}
	if _, err := v.pathspec("/abs/x.go"); err == nil {
		t.Error("pathspec should fail when Rel fails")
	}
	if got := v.WorkspacePath("/abs/x.go"); got != "/abs/x.go" {
		t.Errorf("WorkspacePath should return the input when Rel fails, got %q", got)
	}
}

func TestDaytonaShutdownDeleteError(t *testing.T) {
	sb := newFakeSandbox("sb")
	sb.deleteErr = fmt.Errorf("503")
	v := &DaytonaSandbox{Name: "sb", Managed: true, sb: sb}
	if err := v.Shutdown(); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("expected the delete error, got %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("disk full") }

func TestPackWorkspaceErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []hostCall
	// git ls-files fails.
	bad := func(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
		return []byte("fatal: not a git repository"), fmt.Errorf("exit 128")
	}
	if _, err := packWorkspace(ctx, dir, bad); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("expected ls-files error, got %v", err)
	}
	// A listed path under a regular file: Lstat fails with something other
	// than not-exist, and the packer must say so rather than skip it.
	if _, err := packWorkspace(ctx, dir, recordingHost(&calls, "f.txt/child\x00", nil)); err == nil {
		t.Error("expected an Lstat error for a path under a file")
	}
	// An unreadable file.
	if os.Getuid() != 0 {
		locked := filepath.Join(dir, "locked.txt")
		if err := os.WriteFile(locked, []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := packWorkspace(ctx, dir, recordingHost(&calls, "locked.txt\x00", nil)); err == nil {
			t.Error("expected an open error for an unreadable file")
		}
	}
	// The destination refuses writes: the first tar header fails, and with
	// nothing listed the trailer fails instead.
	if err := packWorkspaceTo(ctx, failingWriter{}, dir, recordingHost(&calls, "f.txt\x00", nil)); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected the writer error through WriteHeader, got %v", err)
	}
	if err := packWorkspaceTo(ctx, failingWriter{}, dir, recordingHost(&calls, "", nil)); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected the writer error through Close, got %v", err)
	}
}

func TestBootDaytonaClaudeInstallFailure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	sb := newFakeSandbox("sb")
	sb.respond = func(command string) (int, string, error) {
		if command == daytonaInit {
			return 1, "npm ERR! network", nil
		}
		return 0, "", nil
	}
	_, err := bootDaytona("n", "/h", "", "", &fakeClient{sb: sb}, emptyHost)
	if err == nil || !strings.Contains(err.Error(), "installing claude") {
		t.Fatalf("expected an install error, got %v", err)
	}
	if !sb.deleted {
		t.Error("sandbox should be deleted when claude cannot be installed")
	}
}

func TestPackWorkspaceSkipsDirectories(t *testing.T) {
	// git ls-files reports a submodule as its directory path; there is
	// nothing to archive for it.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "vendor-sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []hostCall
	r, err := packWorkspace(context.Background(), dir, recordingHost(&calls, "vendor-sub\x00", nil))
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tar.NewReader(gz).Next(); err != io.EOF {
		t.Errorf("expected an empty archive, got %v", err)
	}
}
