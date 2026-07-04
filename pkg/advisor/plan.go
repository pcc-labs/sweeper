// Package advisor implements the optional sweep-planning phase: a frontier
// model reads the lint output (never file contents) and produces a structured
// plan — task ordering, difficulty, strategy hints, and worker-tier
// suggestions — that the orchestrator overlays on the mechanical file
// grouping. Any advisor failure falls back to the mechanical plan.
package advisor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/papercomputeco/sweeper/pkg/planner"
)

// PlannedTask is one file's entry in the advisor's sweep plan.
type PlannedTask struct {
	File       string `json:"file"`
	Difficulty string `json:"difficulty,omitempty"` // easy | medium | hard
	Strategy   string `json:"strategy,omitempty"`   // standard | retry | exploration
	Tier       string `json:"tier,omitempty"`       // suggested worker model (hint only)
}

// Plan is the structured output the advisor model returns.
type Plan struct {
	Tasks []PlannedTask `json:"tasks"`
}

// ParsePlan extracts the JSON plan from raw model output. CLI agents may
// wrap the JSON in prose or markdown fences, so it tries, in order: the
// whole output, the first ```json fence, and the outermost {...} span.
func ParsePlan(output string) (Plan, error) {
	for _, candidate := range jsonCandidates(output) {
		var plan Plan
		if err := json.Unmarshal([]byte(candidate), &plan); err == nil {
			if len(plan.Tasks) == 0 {
				return Plan{}, fmt.Errorf("advisor plan contains no tasks")
			}
			return plan, nil
		}
	}
	return Plan{}, fmt.Errorf("no JSON plan found in advisor output")
}

// jsonCandidates returns substrings of out that might be the JSON plan,
// in decreasing order of confidence.
func jsonCandidates(out string) []string {
	var candidates []string
	trimmed := strings.TrimSpace(out)
	if trimmed != "" {
		candidates = append(candidates, trimmed)
	}
	if _, after, found := strings.Cut(out, "```json"); found {
		if body, _, found := strings.Cut(after, "```"); found {
			candidates = append(candidates, strings.TrimSpace(body))
		}
	}
	if start := strings.Index(out, "{"); start >= 0 {
		if end := strings.LastIndex(out, "}"); end > start {
			candidates = append(candidates, out[start:end+1])
		}
	}
	return candidates
}

// Apply overlays the advisor's plan on the mechanically-grouped tasks.
// Tasks are reordered to match the plan; files the plan omits keep their
// mechanical order at the end; files the plan invents are dropped. The
// returned map carries per-file hints for files the plan covered.
func Apply(plan Plan, tasks []planner.FixTask) ([]planner.FixTask, map[string]PlannedTask) {
	byFile := make(map[string]planner.FixTask, len(tasks))
	for _, t := range tasks {
		byFile[t.File] = t
	}

	ordered := make([]planner.FixTask, 0, len(tasks))
	hints := make(map[string]PlannedTask)
	taken := make(map[string]bool, len(tasks))
	for _, pt := range plan.Tasks {
		t, ok := byFile[pt.File]
		if !ok || taken[pt.File] {
			continue
		}
		taken[pt.File] = true
		ordered = append(ordered, t)
		hints[pt.File] = pt
	}
	for _, t := range tasks {
		if !taken[t.File] {
			ordered = append(ordered, t)
		}
	}
	return ordered, hints
}
