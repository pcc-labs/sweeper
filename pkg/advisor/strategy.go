package advisor

import "github.com/papercomputeco/sweeper/pkg/loop"

// ResolveStrategy converts an advisor strategy hint into a loop.Strategy.
// Retry and exploration prompts embed the prior round's output, so those
// hints are only honored when history exists. Invalid or unusable hints
// fall back to the mechanical loop.PickStrategy.
func ResolveStrategy(hint string, round int, fh loop.FileHistory, staleThreshold int) loop.Strategy {
	switch hint {
	case "standard":
		return loop.StrategyStandard
	case "retry":
		if len(fh.Rounds) > 0 {
			return loop.StrategyRetry
		}
	case "exploration":
		if len(fh.Rounds) > 0 {
			return loop.StrategyExploration
		}
	}
	return loop.PickStrategy(round, fh, staleThreshold)
}
