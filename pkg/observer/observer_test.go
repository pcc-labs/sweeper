package observer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papercomputeco/sweeper/pkg/telemetry"
)

func writeEvents(t *testing.T, dir string, events []telemetry.Event) {
	t.Helper()
	_ = os.MkdirAll(dir, 0o755)
	f, _ := os.Create(filepath.Join(dir, "2026-03-13.jsonl"))
	defer func() { _ = f.Close() }()
	for _, e := range events {
		data, _ := json.Marshal(e)
		_, _ = f.Write(append(data, '\n'))
	}
}

func TestObserveSuccessRate(t *testing.T) {
	dir := t.TempDir()
	events := []telemetry.Event{
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": true}},
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": true}},
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": false}},
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"linter": "ineffassign", "success": true}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) == 0 {
		t.Fatal("expected at least one insight")
	}
}

func TestObserveEmptyDir(t *testing.T) {
	dir := t.TempDir()
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 0 {
		t.Fatalf("expected 0 insights from empty dir, got %d", len(insights))
	}
}

func TestObserveTokensFromTelemetry(t *testing.T) {
	dir := t.TempDir()
	events := []telemetry.Event{
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{
			"linter": "revive", "success": true, "prompt_tokens": 100, "output_tokens": 50}},
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{
			"linter": "revive", "success": false, "prompt_tokens": 200, "output_tokens": 25}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 1 {
		t.Fatalf("expected 1 insight, got %d", len(insights))
	}
	// 100+50 + 200+25 = 375
	if insights[0].TotalTokens != 375 {
		t.Errorf("expected TotalTokens=375 aggregated from telemetry, got %d", insights[0].TotalTokens)
	}
}

func TestObserveZeroTokensWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	events := []telemetry.Event{
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": true}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	for _, ins := range insights {
		if ins.TotalTokens != 0 {
			t.Errorf("expected TotalTokens=0 when telemetry has no token fields, got %d", ins.TotalTokens)
		}
	}
}

func TestEventInt(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"float64", float64(42), 42},
		{"int", 7, 7},
		{"int64", int64(9), 9},
		{"unsupported", "nope", 0},
		{"nil", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eventInt(c.in); got != c.want {
				t.Errorf("eventInt(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestComputeInsightsUnknownLinter(t *testing.T) {
	dir := t.TempDir()
	events := []telemetry.Event{
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"success": true}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 1 {
		t.Fatalf("expected 1 insight, got %d", len(insights))
	}
	if insights[0].Linter != "unknown" {
		t.Errorf("expected linter 'unknown', got %s", insights[0].Linter)
	}
}

func TestComputeInsightsNonFixEvent(t *testing.T) {
	dir := t.TempDir()
	events := []telemetry.Event{
		{Timestamp: time.Now(), Type: "other_event", Data: map[string]any{"linter": "revive"}},
		{Timestamp: time.Now(), Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": true}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 1 {
		t.Fatalf("expected 1 insight (non-fix event skipped), got %d", len(insights))
	}
}

func TestReadFileBadJSON(t *testing.T) {
	dir := t.TempDir()
	// Write a file with invalid JSON lines.
	f, _ := os.Create(filepath.Join(dir, "bad.jsonl"))
	_, _ = f.WriteString("not valid json\n")
	_, _ = f.WriteString(`{"timestamp":"2026-03-13T00:00:00Z","type":"fix_attempt","data":{"linter":"revive","success":true}}` + "\n")
	_ = f.Close()
	obs := New(dir)
	insights, err := obs.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	// Bad line is skipped, valid line is parsed.
	if len(insights) != 1 {
		t.Fatalf("expected 1 insight from valid line, got %d", len(insights))
	}
}

func TestReadFileOpenError(t *testing.T) {
	dir := t.TempDir()
	// Create a .jsonl file that cannot be read.
	path := filepath.Join(dir, "unreadable.jsonl")
	_ = os.WriteFile(path, []byte("data"), 0o644)
	_ = os.Chmod(path, 0o000)
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	obs := New(dir)
	_, err := obs.Analyze()
	if err == nil {
		t.Error("expected error when reading unreadable file")
	}
}

func TestAnalyzeHistoryEmpty(t *testing.T) {
	dir := t.TempDir()
	obs := New(dir)
	hist, err := obs.AnalyzeHistory()
	if err != nil {
		t.Fatal(err)
	}
	if hist.TotalRuns != 0 {
		t.Errorf("expected 0 runs, got %d", hist.TotalRuns)
	}
}

func TestAnalyzeHistoryLegacyEvents(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	events := []telemetry.Event{
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": true}},
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"linter": "revive", "success": false}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	hist, err := obs.AnalyzeHistory()
	if err != nil {
		t.Fatal(err)
	}
	if hist.TotalRuns != 1 {
		t.Errorf("expected 1 run, got %d", hist.TotalRuns)
	}
	if len(hist.SuccessRateTrend) != 1 {
		t.Fatalf("expected 1 trend point, got %d", len(hist.SuccessRateTrend))
	}
	if hist.SuccessRateTrend[0] != 0.5 {
		t.Errorf("expected 0.5 success rate, got %f", hist.SuccessRateTrend[0])
	}
	// Legacy events default to round=1, strategy=standard
	if rate, ok := hist.RoundEffectiveness[1]; !ok || rate != 1.0 {
		t.Errorf("expected round 1 = 1.0, got %v", hist.RoundEffectiveness)
	}
	if rate, ok := hist.StrategyEffectiveness["standard"]; !ok || rate != 0.5 {
		t.Errorf("expected standard = 0.5, got %v", hist.StrategyEffectiveness)
	}
}

func TestAnalyzeHistoryWithRounds(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	events := []telemetry.Event{
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": true, "round": float64(1), "strategy": "standard"}},
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": false, "round": float64(1), "strategy": "standard"}},
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": true, "round": float64(2), "strategy": "retry"}},
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": false, "round": float64(3), "strategy": "exploration"}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	hist, err := obs.AnalyzeHistory()
	if err != nil {
		t.Fatal(err)
	}
	// 2 successes total: 1 in round 1, 1 in round 2
	if hist.RoundEffectiveness[1] != 0.5 {
		t.Errorf("expected round 1 = 0.5, got %f", hist.RoundEffectiveness[1])
	}
	if hist.RoundEffectiveness[2] != 0.5 {
		t.Errorf("expected round 2 = 0.5, got %f", hist.RoundEffectiveness[2])
	}
	// Strategy: standard 1/2=0.5, retry 1/1=1.0, exploration 0/1=0.0
	if hist.StrategyEffectiveness["standard"] != 0.5 {
		t.Errorf("expected standard = 0.5, got %f", hist.StrategyEffectiveness["standard"])
	}
	if hist.StrategyEffectiveness["retry"] != 1.0 {
		t.Errorf("expected retry = 1.0, got %f", hist.StrategyEffectiveness["retry"])
	}
	if hist.StrategyEffectiveness["exploration"] != 0.0 {
		t.Errorf("expected exploration = 0.0, got %f", hist.StrategyEffectiveness["exploration"])
	}
}

func TestAnalyzeHistoryMultipleRuns(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o755)
	// Write two date files to simulate two runs
	ts1 := time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)

	f1, _ := os.Create(filepath.Join(dir, "2026-03-12.jsonl"))
	e1 := telemetry.Event{Timestamp: ts1, Type: "fix_attempt", Data: map[string]any{"success": false}}
	data1, _ := json.Marshal(e1)
	_, _ = f1.Write(append(data1, '\n'))
	_ = f1.Close()

	f2, _ := os.Create(filepath.Join(dir, "2026-03-13.jsonl"))
	e2 := telemetry.Event{Timestamp: ts2, Type: "fix_attempt", Data: map[string]any{"success": true}}
	data2, _ := json.Marshal(e2)
	_, _ = f2.Write(append(data2, '\n'))
	_ = f2.Close()

	obs := New(dir)
	hist, err := obs.AnalyzeHistory()
	if err != nil {
		t.Fatal(err)
	}
	if hist.TotalRuns != 2 {
		t.Errorf("expected 2 runs, got %d", hist.TotalRuns)
	}
	if len(hist.SuccessRateTrend) != 2 {
		t.Fatalf("expected 2 trend points, got %d", len(hist.SuccessRateTrend))
	}
}

func TestAnalyzeHistoryNoSuccesses(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	events := []telemetry.Event{
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": false}},
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": false}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	hist, err := obs.AnalyzeHistory()
	if err != nil {
		t.Fatal(err)
	}
	// No successes means no round effectiveness entries
	if len(hist.RoundEffectiveness) != 0 {
		t.Errorf("expected empty round effectiveness, got %v", hist.RoundEffectiveness)
	}
}

func TestAnalyzeHistorySkipsNonFixEvents(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	events := []telemetry.Event{
		{Timestamp: ts, Type: "round_complete", Data: map[string]any{"round": float64(1)}},
		{Timestamp: ts, Type: "fix_attempt", Data: map[string]any{"success": true}},
	}
	writeEvents(t, dir, events)
	obs := New(dir)
	hist, err := obs.AnalyzeHistory()
	if err != nil {
		t.Fatal(err)
	}
	if hist.TotalRuns != 1 {
		t.Errorf("expected 1 run (round_complete skipped), got %d", hist.TotalRuns)
	}
}

func TestAnalyzeHistoryReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.jsonl")
	_ = os.WriteFile(path, []byte("data"), 0o644)
	_ = os.Chmod(path, 0o000)
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	obs := New(dir)
	_, err := obs.AnalyzeHistory()
	if err == nil {
		t.Error("expected error reading unreadable file")
	}
}

func TestAnalyzeModels(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"timestamp":"2026-07-04T10:00:00Z","type":"fix_attempt","data":{"model":"qwen2.5-coder:7b","provider":"ollama","success":true,"prompt_tokens":100,"output_tokens":50}}`,
		`{"timestamp":"2026-07-04T10:01:00Z","type":"fix_attempt","data":{"model":"qwen2.5-coder:7b","provider":"ollama","success":false,"prompt_tokens":100,"output_tokens":50}}`,
		`{"timestamp":"2026-07-04T10:02:00Z","type":"fix_attempt","data":{"model":"claude-haiku-4-5","provider":"claude","success":true}}`,
		`{"timestamp":"2026-07-04T10:03:00Z","type":"advisor_plan","data":{"model":"claude-opus-4-8","success":true}}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	insights, err := New(dir).AnalyzeModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 2 {
		t.Fatalf("expected 2 models (advisor_plan excluded), got %d: %+v", len(insights), insights)
	}
	// Sorted by attempts desc: qwen first.
	q := insights[0]
	if q.Model != "qwen2.5-coder:7b" || q.Attempts != 2 || q.Successes != 1 || q.SuccessRate != 0.5 || q.TotalTokens != 300 {
		t.Errorf("unexpected qwen stats: %+v", q)
	}
	h := insights[1]
	if h.Model != "claude-haiku-4-5" || h.Provider != "claude" || h.Attempts != 1 || h.SuccessRate != 1.0 {
		t.Errorf("unexpected haiku stats: %+v", h)
	}
}

func TestAnalyzeModelsEmptyModelBucketsAsDefault(t *testing.T) {
	dir := t.TempDir()
	line := `{"timestamp":"2026-07-04T10:00:00Z","type":"fix_attempt","data":{"model":"","provider":"claude","success":true}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	insights, err := New(dir).AnalyzeModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 1 || insights[0].Model != "(default)" {
		t.Errorf("expected empty model bucketed as (default), got %+v", insights)
	}
}

func TestAnalyzeModelsReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.jsonl")
	_ = os.WriteFile(path, []byte("data"), 0o644)
	_ = os.Chmod(path, 0o000)
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	if _, err := New(dir).AnalyzeModels(); err == nil {
		t.Error("expected error reading unreadable file")
	}
}

func TestAnalyzeModelsSortsByModelOnEqualAttempts(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"timestamp":"2026-07-04T10:00:00Z","type":"fix_attempt","data":{"model":"zeta","provider":"claude","success":true}}`,
		`{"timestamp":"2026-07-04T10:01:00Z","type":"fix_attempt","data":{"model":"alpha","provider":"claude","success":true}}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	insights, err := New(dir).AnalyzeModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 2 || insights[0].Model != "alpha" || insights[1].Model != "zeta" {
		t.Errorf("expected tie broken by model name (alpha, zeta), got %+v", insights)
	}
}
