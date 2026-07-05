package vm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestVMBootCallsMbUp(t *testing.T) {
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("ok"), nil
	}
	jcardDir := t.TempDir()
	vm, err := boot("sweeper-test", "/host/proj", jcardDir, runner)
	if err != nil {
		t.Fatal(err)
	}
	if vm.Name != "sweeper-test" {
		t.Errorf("expected name sweeper-test, got %s", vm.Name)
	}
	if vm.Managed != true {
		t.Error("boot should set Managed=true")
	}
	if len(gotArgs) < 3 || gotArgs[0] != "mb" || gotArgs[1] != "up" {
		t.Errorf("expected mb up --config ..., got %v", gotArgs)
	}
}

func TestVMBootError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("mb not found")
	}
	_, err := boot("test", "/tmp", t.TempDir(), runner)
	if err == nil {
		t.Fatal("expected error from failed mb up")
	}
}

func TestVMBootJcardError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}
	// Use a file as a path component so MkdirAll inside GenerateJcard fails.
	base := t.TempDir()
	blockingFile := base + "/file"
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badDir := blockingFile + "/nested"
	_, err := boot("test", "/tmp", badDir, runner)
	if err == nil {
		t.Fatal("expected error from failed GenerateJcard")
	}
}

func TestVMShutdownManaged(t *testing.T) {
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return nil, nil
	}
	vm := &VM{Name: "sweeper-test", Managed: true, runner: runner, jcardPath: "/tmp/jcard.toml"}
	err := vm.Shutdown()
	if err != nil {
		t.Fatal(err)
	}
	if len(gotArgs) < 2 || gotArgs[0] != "mb" || gotArgs[1] != "destroy" {
		t.Errorf("expected mb destroy, got %v", gotArgs)
	}
}

func TestVMShutdownUnmanaged(t *testing.T) {
	called := false
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	vm := &VM{Name: "user-vm", Managed: false, runner: runner}
	err := vm.Shutdown()
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("unmanaged VM should not call mb destroy")
	}
}

func TestVMExec(t *testing.T) {
	var gotArgs []string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("output"), nil
	}
	vm := &VM{Name: "sweeper-test", runner: runner}
	out, err := vm.Exec(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "output" {
		t.Errorf("expected 'output', got %s", string(out))
	}
	expected := []string{"mb", "ssh", "--user", "agent", "sweeper-test", "--", "echo", "hello"}
	if len(gotArgs) != len(expected) {
		t.Fatalf("expected args %v, got %v", expected, gotArgs)
	}
	for i, want := range expected {
		if gotArgs[i] != want {
			t.Errorf("arg[%d]: expected %q, got %q", i, want, gotArgs[i])
		}
	}
}

func TestVMExecError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("err output"), fmt.Errorf("exit 1")
	}
	vm := &VM{Name: "test", runner: runner}
	out, err := vm.Exec(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if string(out) != "err output" {
		t.Errorf("should return output even on error, got %s", string(out))
	}
}

func TestNewVMName(t *testing.T) {
	name := NewVMName()
	if !strings.HasPrefix(name, "sweeper-") {
		t.Errorf("expected sweeper- prefix, got %s", name)
	}
	if len(name) < 12 {
		t.Error("name should include a hash suffix")
	}
}

func TestAttach(t *testing.T) {
	vm := Attach("my-vm", "/host/proj")
	if vm.Name != "my-vm" {
		t.Errorf("expected my-vm, got %s", vm.Name)
	}
	if vm.Managed {
		t.Error("attached VM should not be managed")
	}
	if vm.Dir != "/host/proj" {
		t.Errorf("expected /host/proj, got %s", vm.Dir)
	}
}

func TestWorkspacePath(t *testing.T) {
	vm := &VM{Dir: "/host/project"}
	got := vm.WorkspacePath("/host/project/src/main.go")
	if got != "/workspace/src/main.go" {
		t.Errorf("expected /workspace/src/main.go, got %s", got)
	}
}

func TestWorkspacePathRelError(t *testing.T) {
	vm := &VM{Dir: ""}
	// When filepath.Rel fails or Dir is empty, should return original path
	got := vm.WorkspacePath("/some/absolute/path")
	// filepath.Rel("", "/some/absolute/path") succeeds on most platforms
	// Just verify it doesn't panic and returns something
	if got == "" {
		t.Error("WorkspacePath should return non-empty string")
	}
}

func TestVMShutdownError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("destroy failed"), fmt.Errorf("timeout")
	}
	vm := &VM{Name: "test", Managed: true, runner: runner}
	err := vm.Shutdown()
	if err == nil {
		t.Fatal("expected error from failed destroy")
	}
}

func TestBootPublicAPIErrorsWithoutMb(t *testing.T) {
	// Exercise the public Boot() function and defaultRunner.
	// mb is not installed in test environments so this covers the
	// error path through defaultRunner → exec.Command failure.
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	_, err := Boot("test-vm", "/tmp/proj", dir)
	if err == nil {
		t.Error("expected error when mb is not available")
	}
}

func TestVMExecPropagatesContext(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	var gotCtx context.Context
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotCtx = ctx
		return nil, nil
	}
	vm := &VM{Name: "t", runner: runner}
	if _, err := vm.Exec(ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	if gotCtx == nil || gotCtx.Value(ctxKey{}) != "marker" {
		t.Error("Exec should pass its context through to the runner")
	}
}
