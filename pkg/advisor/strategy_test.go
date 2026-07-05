package advisor

import (
	"testing"

	"github.com/papercomputeco/sweeper/pkg/loop"
)

func TestResolveStrategyHonorsStandardHint(t *testing.T) {
	// Round 1 with stagnant history would mechanically pick exploration;
	// the advisor can pin it back to standard.
	fh := loop.FileHistory{Rounds: []loop.RoundResult{{Fixed: 0}, {Fixed: 0}}}
	got := ResolveStrategy("standard", 2, fh, 2)
	if got != loop.StrategyStandard {
		t.Errorf("expected standard, got %s", got)
	}
}

func TestResolveStrategyHonorsExplorationWithHistory(t *testing.T) {
	fh := loop.FileHistory{Rounds: []loop.RoundResult{{Fixed: 1}}}
	got := ResolveStrategy("exploration", 1, fh, 99)
	if got != loop.StrategyExploration {
		t.Errorf("expected exploration, got %s", got)
	}
}

func TestResolveStrategyDowngradesWithoutHistory(t *testing.T) {
	// Retry/exploration prompts embed prior output; with no history there is
	// none, so the hint falls back to the mechanical pick (standard at round 0).
	for _, hint := range []string{"retry", "exploration"} {
		got := ResolveStrategy(hint, 0, loop.FileHistory{}, 2)
		if got != loop.StrategyStandard {
			t.Errorf("hint %q without history: expected standard, got %s", hint, got)
		}
	}
}

func TestResolveStrategyInvalidHintFallsBack(t *testing.T) {
	// Unrecognized hint → mechanical pick. Round 1 with improving history → retry.
	fh := loop.FileHistory{Rounds: []loop.RoundResult{{Fixed: 1}}}
	got := ResolveStrategy("yolo", 1, fh, 2)
	if got != loop.StrategyRetry {
		t.Errorf("expected mechanical retry, got %s", got)
	}
}

func TestResolveStrategyEmptyHintFallsBack(t *testing.T) {
	got := ResolveStrategy("", 0, loop.FileHistory{}, 2)
	if got != loop.StrategyStandard {
		t.Errorf("expected standard, got %s", got)
	}
}

func TestResolveStrategyHonorsRetryWithHistory(t *testing.T) {
	fh := loop.FileHistory{Rounds: []loop.RoundResult{{Fixed: 1}}}
	got := ResolveStrategy("retry", 1, fh, 99)
	if got != loop.StrategyRetry {
		t.Errorf("expected retry, got %s", got)
	}
}
