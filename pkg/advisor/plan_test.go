package advisor

import (
	"strings"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/planner"
)

func TestParsePlanRawJSON(t *testing.T) {
	out := `{"tasks":[{"file":"a.go","difficulty":"easy","strategy":"standard","tier":"claude-haiku-4-5"},{"file":"b.go","difficulty":"hard","strategy":"exploration"}]}`
	plan, err := ParsePlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].File != "a.go" || plan.Tasks[0].Tier != "claude-haiku-4-5" {
		t.Errorf("unexpected first task: %+v", plan.Tasks[0])
	}
	if plan.Tasks[1].Strategy != "exploration" {
		t.Errorf("unexpected second task: %+v", plan.Tasks[1])
	}
}

func TestParsePlanFencedJSON(t *testing.T) {
	out := "Here is the sweep plan:\n\n```json\n{\"tasks\":[{\"file\":\"a.go\"}]}\n```\nGood luck!"
	plan, err := ParsePlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || plan.Tasks[0].File != "a.go" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestParsePlanEmbeddedJSON(t *testing.T) {
	// CLI agents often wrap JSON in prose without fences.
	out := "I analyzed the issues.\n{\"tasks\":[{\"file\":\"a.go\",\"difficulty\":\"medium\"}]}\nDone."
	plan, err := ParsePlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || plan.Tasks[0].Difficulty != "medium" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestParsePlanInvalid(t *testing.T) {
	if _, err := ParsePlan("no json here at all"); err == nil {
		t.Error("expected error for non-JSON output")
	}
	if _, err := ParsePlan(""); err == nil {
		t.Error("expected error for empty output")
	}
}

func TestParsePlanEmptyTasks(t *testing.T) {
	_, err := ParsePlan(`{"tasks":[]}`)
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Errorf("expected 'no tasks' error, got %v", err)
	}
}

func fixTasks(files ...string) []planner.FixTask {
	tasks := make([]planner.FixTask, len(files))
	for i, f := range files {
		tasks[i] = planner.FixTask{
			File:   f,
			Issues: []linter.Issue{{File: f, Line: 1, Linter: "revive", Message: "msg"}},
		}
	}
	return tasks
}

func TestApplyReordersTasks(t *testing.T) {
	plan := Plan{Tasks: []PlannedTask{{File: "b.go", Difficulty: "hard"}, {File: "a.go"}}}
	ordered, hints := Apply(plan, fixTasks("a.go", "b.go"))
	if ordered[0].File != "b.go" || ordered[1].File != "a.go" {
		t.Errorf("expected plan order [b.go a.go], got [%s %s]", ordered[0].File, ordered[1].File)
	}
	if hints["b.go"].Difficulty != "hard" {
		t.Errorf("expected hint for b.go, got %+v", hints["b.go"])
	}
}

func TestApplyDropsUnknownFiles(t *testing.T) {
	plan := Plan{Tasks: []PlannedTask{{File: "hallucinated.go"}, {File: "a.go"}}}
	ordered, _ := Apply(plan, fixTasks("a.go"))
	if len(ordered) != 1 || ordered[0].File != "a.go" {
		t.Errorf("expected hallucinated file dropped, got %+v", ordered)
	}
}

func TestApplyAppendsOmittedFiles(t *testing.T) {
	plan := Plan{Tasks: []PlannedTask{{File: "c.go"}}}
	ordered, hints := Apply(plan, fixTasks("a.go", "b.go", "c.go"))
	if len(ordered) != 3 {
		t.Fatalf("expected all 3 tasks kept, got %d", len(ordered))
	}
	// c.go planned first, then a.go and b.go keep mechanical order.
	if ordered[0].File != "c.go" || ordered[1].File != "a.go" || ordered[2].File != "b.go" {
		t.Errorf("unexpected order: %s %s %s", ordered[0].File, ordered[1].File, ordered[2].File)
	}
	if _, ok := hints["a.go"]; ok {
		t.Error("expected no hint for omitted file a.go")
	}
}

func TestApplyIgnoresDuplicatePlanEntries(t *testing.T) {
	plan := Plan{Tasks: []PlannedTask{{File: "a.go"}, {File: "a.go", Difficulty: "hard"}}}
	ordered, hints := Apply(plan, fixTasks("a.go", "b.go"))
	if len(ordered) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(ordered))
	}
	// First entry wins.
	if hints["a.go"].Difficulty != "" {
		t.Errorf("expected first entry's hint kept, got %+v", hints["a.go"])
	}
}
