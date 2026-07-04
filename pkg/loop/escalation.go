package loop

// Escalation tracks each file's position on the model escalation ladder
// for one run. Rung 0 is the base worker; top is the highest rung index
// (the ladder length). State never persists across runs.
type Escalation struct {
	top   int
	rungs map[string]int
}

// NewEscalation returns per-file rung state with rungs 0..top available.
func NewEscalation(top int) *Escalation {
	if top < 0 {
		top = 0
	}
	return &Escalation{top: top, rungs: make(map[string]int)}
}

// Rung returns the file's current rung (0 = base worker).
func (e *Escalation) Rung(file string) int {
	return e.rungs[file]
}

// Seed raises the file's starting rung (e.g. from an advisor tier hint).
// It never demotes, and caps at the top rung.
func (e *Escalation) Seed(file string, rung int) {
	if rung > e.top {
		rung = e.top
	}
	if rung > e.rungs[file] {
		e.rungs[file] = rung
	}
}

// Bump moves the file up one rung, capped at the top, and returns the
// resulting rung.
func (e *Escalation) Bump(file string) int {
	if e.rungs[file] < e.top {
		e.rungs[file]++
	}
	return e.rungs[file]
}

// AtTop reports whether the file has no stronger rung left to climb to.
func (e *Escalation) AtTop(file string) bool {
	return e.rungs[file] >= e.top
}
