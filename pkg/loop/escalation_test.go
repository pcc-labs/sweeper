package loop

import "testing"

func TestEscalationStartsAtBase(t *testing.T) {
	e := NewEscalation(2)
	if e.Rung("a.go") != 0 {
		t.Errorf("expected rung 0, got %d", e.Rung("a.go"))
	}
	if e.AtTop("a.go") {
		t.Error("expected not at top with 2 rungs available")
	}
}

func TestEscalationBumpCapsAtTop(t *testing.T) {
	e := NewEscalation(2)
	if got := e.Bump("a.go"); got != 1 {
		t.Errorf("expected rung 1 after first bump, got %d", got)
	}
	if got := e.Bump("a.go"); got != 2 {
		t.Errorf("expected rung 2 after second bump, got %d", got)
	}
	if !e.AtTop("a.go") {
		t.Error("expected at top after reaching rung 2")
	}
	if got := e.Bump("a.go"); got != 2 {
		t.Errorf("expected bump capped at 2, got %d", got)
	}
}

func TestEscalationSeedOnlyRaises(t *testing.T) {
	e := NewEscalation(3)
	e.Seed("a.go", 2)
	if e.Rung("a.go") != 2 {
		t.Errorf("expected seeded rung 2, got %d", e.Rung("a.go"))
	}
	e.Seed("a.go", 1) // lower seed must not demote
	if e.Rung("a.go") != 2 {
		t.Errorf("expected rung to stay 2, got %d", e.Rung("a.go"))
	}
	e.Seed("a.go", 9) // capped at top
	if e.Rung("a.go") != 3 {
		t.Errorf("expected seed capped at 3, got %d", e.Rung("a.go"))
	}
}

func TestEscalationZeroTopAlwaysAtTop(t *testing.T) {
	e := NewEscalation(0)
	if !e.AtTop("a.go") {
		t.Error("with no rungs above base, every file is at top")
	}
	if got := e.Bump("a.go"); got != 0 {
		t.Errorf("expected bump no-op at 0, got %d", got)
	}
}

func TestEscalationFilesIndependent(t *testing.T) {
	e := NewEscalation(2)
	e.Bump("a.go")
	if e.Rung("b.go") != 0 {
		t.Errorf("expected b.go untouched at rung 0, got %d", e.Rung("b.go"))
	}
}
