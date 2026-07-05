package advisor

import (
	"strings"
	"testing"

	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/planner"
)

func TestBuildPromptListsFilesAndIssues(t *testing.T) {
	tasks := []planner.FixTask{
		{File: "auth.go", Issues: []linter.Issue{
			{File: "auth.go", Line: 42, Linter: "ineffassign", Message: "err is not used"},
		}},
		{File: "router.go", Issues: []linter.Issue{
			{File: "router.go", Line: 7, Linter: "revive", Message: "exported func needs comment"},
		}},
	}
	prompt := BuildPrompt(tasks, nil, 0, nil)
	for _, want := range []string{"auth.go", "router.go", "Line 42", "ineffassign", "err is not used"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildPromptRequestsJSONSchema(t *testing.T) {
	prompt := BuildPrompt(fixTasks("a.go"), nil, 0, nil)
	for _, want := range []string{`"tasks"`, `"file"`, `"difficulty"`, `"strategy"`, `"tier"`, "ONLY with JSON"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing schema element %q", want)
		}
	}
}

func TestBuildPromptForbidsFileAccess(t *testing.T) {
	prompt := BuildPrompt(fixTasks("a.go"), nil, 0, nil)
	if !strings.Contains(prompt, "Do not read or modify any files") {
		t.Error("prompt must forbid file access — the advisor plans from lint output only")
	}
}

func TestBuildPromptIncludesHistoryOnLaterRounds(t *testing.T) {
	histories := map[string]loop.FileHistory{
		"a.go": {File: "a.go", Rounds: []loop.RoundResult{
			{Round: 0, Strategy: loop.StrategyStandard, Fixed: 0},
		}},
	}
	prompt := BuildPrompt(fixTasks("a.go"), histories, 1, nil)
	if !strings.Contains(prompt, "round 2") {
		t.Errorf("prompt should state the upcoming round, got: %s", prompt)
	}
	if !strings.Contains(prompt, "1 prior attempt") {
		t.Errorf("prompt should summarize history, got: %s", prompt)
	}
}

func TestBuildPromptOmitsHistoryOnRoundZero(t *testing.T) {
	prompt := BuildPrompt(fixTasks("a.go"), nil, 0, nil)
	if strings.Contains(prompt, "prior attempt") {
		t.Error("round 0 prompt should not mention history")
	}
}

func TestBuildPromptListsWorkerTiers(t *testing.T) {
	prompt := BuildPrompt(fixTasks("a.go"), nil, 0, []string{"qwen2.5-coder:7b", "claude-haiku-4-5"})
	if !strings.Contains(prompt, "qwen2.5-coder:7b") || !strings.Contains(prompt, "claude-haiku-4-5") {
		t.Error("prompt should list the available worker tiers")
	}
	if !strings.Contains(prompt, `"tier"`) {
		t.Error("prompt should keep the tier schema field")
	}
}

func TestBuildPromptOmitsTierGuidanceWithoutTiers(t *testing.T) {
	prompt := BuildPrompt(fixTasks("a.go"), nil, 0, nil)
	if strings.Contains(prompt, "Available worker tiers") {
		t.Error("prompt should not mention tiers when none are configured")
	}
}
