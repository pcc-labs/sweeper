package advisor

import (
	"strings"
	"testing"
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
