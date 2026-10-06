package vm

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type call []string

func recordingRunner(calls *[]call, failOn string) cmdRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		c := append(call{name}, args...)
		*calls = append(*calls, c)
		if failOn != "" && len(args) >= 2 && args[1] == failOn {
			return []byte("boom"), fmt.Errorf("exit 1")
		}
		return []byte("ok"), nil
	}
}

func has(c call, want ...string) bool {
	s := strings.Join(c, "\x00")
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func TestBootSmolCreatesThenStarts(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	var calls []call
	v, err := bootSmol("sweeper-t", "/host/proj", "", recordingRunner(&calls, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Managed || v.Image != DefaultSmolImage {
		t.Errorf("got managed=%v image=%q", v.Managed, v.Image)
	}
	if len(calls) != 2 {
		t.Fatalf("expected create + start, got %v", calls)
	}
	create := calls[0]
	if create[0] != "smolvm" || create[1] != "machine" || create[2] != "create" {
		t.Errorf("first call should be smolvm machine create, got %v", create)
	}
	if !has(create, "--name\x00sweeper-t", "--image\x00"+DefaultSmolImage, "-v\x00/host/proj:/workspace", "-w\x00/workspace", "IS_SANDBOX=1") {
		t.Errorf("create args missing name/image/mount/workdir/sandbox: %v", create)
	}
	if !has(create, "--credential\x00"+smolCredential) {
		t.Errorf("create should bind the API key as a credential: %v", create)
	}
	if strings.Contains(strings.Join(create, " "), "sk-test") {
		t.Errorf("the real API key must never appear in VM args: %v", create)
	}
	if !has(calls[1], "machine\x00start\x00--name\x00sweeper-t") {
		t.Errorf("second call should start the machine, got %v", calls[1])
	}
}

func TestBootSmolCustomImage(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	var calls []call
	v, err := bootSmol("n", "/h", "ghcr.io/acme/agent:1", recordingRunner(&calls, ""))
	if err != nil {
		t.Fatal(err)
	}
	if v.Image != "ghcr.io/acme/agent:1" || !has(calls[0], "--image\x00ghcr.io/acme/agent:1") {
		t.Errorf("custom image not used: %v", calls[0])
	}
}

func TestBootSmolRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	var calls []call
	if _, err := bootSmol("n", "/h", "", recordingRunner(&calls, "")); err == nil {
		t.Fatal("expected error without ANTHROPIC_API_KEY")
	}
	if len(calls) != 0 {
		t.Errorf("should not touch smolvm without a key, got %v", calls)
	}
}

func TestBootSmolStartFailureDeletes(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	var calls []call
	if _, err := bootSmol("n", "/h", "", recordingRunner(&calls, "start")); err == nil {
		t.Fatal("expected start error")
	}
	last := calls[len(calls)-1]
	if !has(last, "machine\x00delete\x00--name\x00n", "--force") {
		t.Errorf("failed start should delete the machine, got %v", last)
	}
}

func TestBootSmolCreateFailure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	var calls []call
	if _, err := bootSmol("n", "/h", "", recordingRunner(&calls, "create")); err == nil {
		t.Fatal("expected create error")
	}
	if len(calls) != 1 {
		t.Errorf("should stop after failed create, got %v", calls)
	}
}

func TestSmolExec(t *testing.T) {
	var calls []call
	v := &SmolVM{Name: "sw", runner: recordingRunner(&calls, "")}
	if _, err := v.Exec(context.Background(), "claude", "--print", "hi"); err != nil {
		t.Fatal(err)
	}
	want := []string{"smolvm", "machine", "exec", "--name", "sw", "-w", "/workspace", "--", "claude", "--print", "hi"}
	if strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("exec args:\n got %v\nwant %v", calls[0], want)
	}
}

func TestSmolShutdownManaged(t *testing.T) {
	var calls []call
	v := &SmolVM{Name: "sw", Managed: true, runner: recordingRunner(&calls, "")}
	if err := v.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !has(calls[0], "machine\x00stop") || !has(calls[1], "machine\x00delete", "--force") {
		t.Errorf("expected stop then delete, got %v", calls)
	}
}

func TestSmolShutdownStopFailureKeepsMachine(t *testing.T) {
	var calls []call
	v := &SmolVM{Name: "sw", Managed: true, runner: recordingRunner(&calls, "stop")}
	if err := v.Shutdown(); err == nil {
		t.Fatal("expected stop error")
	}
	if len(calls) != 1 {
		t.Errorf("a failed stop must not delete (it could discard an undrained mount), got %v", calls)
	}
}

func TestSmolShutdownUnmanaged(t *testing.T) {
	var calls []call
	v := &SmolVM{Name: "user", Managed: false, runner: recordingRunner(&calls, "")}
	if err := v.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("attached machine must not be stopped, got %v", calls)
	}
}

func TestSmolWorkspacePath(t *testing.T) {
	v := &SmolVM{Dir: "/host/proj"}
	if got := v.WorkspacePath("/host/proj/src/a.go"); got != "/workspace/src/a.go" {
		t.Errorf("got %s", got)
	}
}

func TestSmolShutdownDeleteFailure(t *testing.T) {
	var calls []call
	v := &SmolVM{Name: "sw", Managed: true, runner: recordingRunner(&calls, "delete")}
	if err := v.Shutdown(); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestSmolWorkspacePathUnrelated(t *testing.T) {
	v := &SmolVM{Dir: "/host/proj"}
	if got := v.WorkspacePath("relative/a.go"); got != "relative/a.go" {
		t.Errorf("a path Rel cannot relate should pass through, got %s", got)
	}
}

func TestAttachSmolIsUnmanaged(t *testing.T) {
	v := AttachSmol("mine", "/h")
	if v.Managed || v.Name != "mine" || v.runner == nil {
		t.Errorf("got %+v", v)
	}
}
