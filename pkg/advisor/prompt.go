package advisor

import (
	"fmt"
	"strings"

	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/planner"
)

// advisorPreamble identifies this as an automated tool, mirroring the worker
// agentPreamble, and pins the advisor to planning-only behavior.
const advisorPreamble = "You are the planning advisor for Sweeper, an automated code maintenance tool. " +
	"A human developer initiated this run and will review all changes. " +
	"Your job is to plan the sweep, not to fix anything. " +
	"Do not read or modify any files, do not run commands, and do not access external services. " +
	"Plan using only the lint output and history below.\n\n"

const planInstructions = "\nRespond ONLY with JSON matching this schema, no prose before or after:\n" +
	"{\"tasks\": [{\"file\": \"<path from the list above>\", " +
	"\"difficulty\": \"easy|medium|hard\", " +
	"\"strategy\": \"standard|retry|exploration\", " +
	"\"tier\": \"<optional: suggested worker model for this file>\"}]}\n\n" +
	"Order tasks by expected leverage: quick unblocking wins first, gnarly refactors last. " +
	"Include every file listed above. Suggest \"exploration\" only for files that got stuck."

// BuildPrompt renders the advisor planning prompt for the upcoming round.
// It includes the lint issues grouped by file and, after round 0, a one-line
// history summary per file — never file contents.
func BuildPrompt(tasks []planner.FixTask, histories map[string]loop.FileHistory, round int, tiers []string) string {
	var b strings.Builder
	b.WriteString(advisorPreamble)
	fmt.Fprintf(&b, "Plan round %d of a lint-fixing sweep across %d files:\n\n", round+1, len(tasks))
	for _, t := range tasks {
		fmt.Fprintf(&b, "%s (%d issues):\n", t.File, len(t.Issues))
		for _, iss := range t.Issues {
			fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
		}
		if round > 0 {
			if fh, ok := histories[t.File]; ok && len(fh.Rounds) > 0 {
				plural := "s"
				if len(fh.Rounds) == 1 {
					plural = ""
				}
				fmt.Fprintf(&b, "History: %d prior attempt%s, last strategy %s, %d consecutive rounds without improvement\n",
					len(fh.Rounds), plural, fh.Rounds[len(fh.Rounds)-1].Strategy, fh.ConsecutiveStale())
			}
		}
		b.WriteString("\n")
	}
	if len(tiers) > 0 {
		fmt.Fprintf(&b, "Available worker tiers, weakest to strongest: %s. "+
			"Set \"tier\" to one of these exact names when a file warrants a stronger starting model.\n",
			strings.Join(tiers, ", "))
	}
	b.WriteString(planInstructions)
	return b.String()
}
