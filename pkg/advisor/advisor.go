package advisor

import (
	"context"
	"fmt"
	"time"

	"github.com/papercomputeco/sweeper/pkg/loop"
	"github.com/papercomputeco/sweeper/pkg/planner"
	"github.com/papercomputeco/sweeper/pkg/worker"
)

// Timeout bounds a single advisor invocation. Planning is one-shot text
// generation; if it takes longer than this the round proceeds mechanically.
const Timeout = 5 * time.Minute

// Advise runs the advisor executor once and returns its parsed sweep plan.
// The caller is responsible for falling back to the mechanical plan on error.
func Advise(ctx context.Context, exec worker.Executor, dir string, tasks []planner.FixTask, histories map[string]loop.FileHistory, round int, tiers []string) (Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	res := exec(ctx, worker.Task{
		ID:     -1, // not a fix task; never collides with dispatch IDs
		Dir:    dir,
		Prompt: BuildPrompt(tasks, histories, round, tiers),
	})
	if !res.Success {
		return Plan{}, fmt.Errorf("advisor execution failed: %s", res.Error)
	}
	return ParsePlan(res.Output)
}
