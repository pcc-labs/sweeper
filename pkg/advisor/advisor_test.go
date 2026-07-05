package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/worker"
)

func TestAdviseParsesExecutorOutput(t *testing.T) {
	var gotTask worker.Task
	exec := func(ctx context.Context, task worker.Task) worker.Result {
		gotTask = task
		return worker.Result{Success: true, Output: `{"tasks":[{"file":"a.go","difficulty":"easy"}]}`}
	}
	plan, err := Advise(context.Background(), exec, "/repo", fixTasks("a.go"), nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || plan.Tasks[0].File != "a.go" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if gotTask.Dir != "/repo" {
		t.Errorf("expected task dir /repo, got %q", gotTask.Dir)
	}
	if !strings.Contains(gotTask.Prompt, "planning advisor") {
		t.Errorf("expected advisor prompt, got: %s", gotTask.Prompt)
	}
}

func TestAdviseErrorsOnExecutorFailure(t *testing.T) {
	exec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: false, Error: "exit status 1"}
	}
	if _, err := Advise(context.Background(), exec, ".", fixTasks("a.go"), nil, 0, nil); err == nil {
		t.Error("expected error when executor fails")
	}
}

func TestAdviseErrorsOnGarbageOutput(t *testing.T) {
	exec := func(ctx context.Context, task worker.Task) worker.Result {
		return worker.Result{Success: true, Output: "I refuse to answer in JSON."}
	}
	if _, err := Advise(context.Background(), exec, ".", fixTasks("a.go"), nil, 0, nil); err == nil {
		t.Error("expected error on unparseable output")
	}
}

func TestAdviseAppliesTimeout(t *testing.T) {
	exec := func(ctx context.Context, task worker.Task) worker.Result {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("expected a deadline on the advisor context")
		}
		return worker.Result{Success: true, Output: `{"tasks":[{"file":"a.go"}]}`}
	}
	if _, err := Advise(context.Background(), exec, ".", fixTasks("a.go"), nil, 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAdvisePassesTiersToPrompt(t *testing.T) {
	var gotPrompt string
	exec := func(ctx context.Context, task worker.Task) worker.Result {
		gotPrompt = task.Prompt
		return worker.Result{Success: true, Output: `{"tasks":[{"file":"a.go"}]}`}
	}
	if _, err := Advise(context.Background(), exec, ".", fixTasks("a.go"), nil, 0, []string{"tier-x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPrompt, "tier-x") {
		t.Errorf("expected tiers threaded into the prompt, got: %s", gotPrompt)
	}
}
