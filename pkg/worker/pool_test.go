package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papercomputeco/sweeper/pkg/linter"
)

func TestPoolRunsTasksConcurrently(t *testing.T) {
	tasks := []Task{
		{ID: 1, File: "a.go", Issues: []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "msg"}}},
		{ID: 2, File: "b.go", Issues: []linter.Issue{{File: "b.go", Line: 2, Linter: "revive", Message: "msg"}}},
	}
	executor := func(ctx context.Context, t Task) Result {
		return Result{TaskID: t.ID, File: t.File, Success: true, IssuesFix: len(t.Issues)}
	}
	pool := NewPool(2, executor)
	results := pool.Run(context.Background(), tasks)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("task %d failed", r.TaskID)
		}
	}
}

func TestPoolRespectsMaxConcurrency(t *testing.T) {
	var maxConcurrent int64
	var current int64
	var mu sync.Mutex
	tasks := make([]Task, 10)
	for i := range tasks {
		tasks[i] = Task{ID: i, File: fmt.Sprintf("%d.go", i)}
	}
	executor := func(ctx context.Context, task Task) Result {
		mu.Lock()
		current++
		if current > maxConcurrent {
			maxConcurrent = current
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		current--
		mu.Unlock()
		return Result{TaskID: task.ID, Success: true}
	}
	pool := NewPool(3, executor)
	pool.Run(context.Background(), tasks)
	if maxConcurrent > 3 {
		t.Errorf("max concurrency exceeded: got %d, want <= 3", maxConcurrent)
	}
}

func TestPoolRunStream(t *testing.T) {
	tasks := []Task{
		{ID: 1, File: "a.go", Issues: []linter.Issue{{File: "a.go", Line: 1, Linter: "revive", Message: "msg"}}},
		{ID: 2, File: "b.go", Issues: []linter.Issue{{File: "b.go", Line: 2, Linter: "revive", Message: "msg"}}},
		{ID: 3, File: "c.go", Issues: []linter.Issue{{File: "c.go", Line: 3, Linter: "revive", Message: "msg"}}},
	}
	executor := func(ctx context.Context, t Task) Result {
		time.Sleep(10 * time.Millisecond)
		return Result{TaskID: t.ID, File: t.File, Success: true, IssuesFix: len(t.Issues)}
	}
	pool := NewPool(2, executor)
	ch := pool.RunStream(context.Background(), tasks)

	var results []Result
	for r := range ch {
		results = append(results, r)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("task %d failed", r.TaskID)
		}
	}
}

// The pool stamps TaskID and File onto results itself so TaskID-keyed
// lookups downstream stay valid even if an executor forgets to set them.
// Output carries the file the executor actually ran, as ground truth.
func TestPoolRunStreamStampsTaskIDAndFile(t *testing.T) {
	tasks := []Task{
		{ID: 4, File: "a.go"},
		{ID: 7, File: "b.go"},
		{ID: 9, File: "c.go"},
	}
	fileByID := make(map[int]string, len(tasks))
	for _, task := range tasks {
		fileByID[task.ID] = task.File
	}
	executor := func(ctx context.Context, task Task) Result {
		return Result{Success: true, Output: task.File}
	}
	pool := NewPool(2, executor)
	for r := range pool.RunStream(context.Background(), tasks) {
		if r.File != r.Output {
			t.Errorf("result for %s stamped with File %q", r.Output, r.File)
		}
		if fileByID[r.TaskID] != r.Output {
			t.Errorf("result for %s stamped with TaskID %d", r.Output, r.TaskID)
		}
		delete(fileByID, r.TaskID)
	}
	if len(fileByID) != 0 {
		t.Errorf("missing results for tasks: %v", fileByID)
	}
}

func TestPoolRunStampsTaskIDAndFile(t *testing.T) {
	tasks := []Task{
		{ID: 3, File: "x.go"},
		{ID: 5, File: "y.go"},
	}
	executor := func(ctx context.Context, task Task) Result {
		return Result{Success: true, Output: task.File}
	}
	pool := NewPool(2, executor)
	results := pool.Run(context.Background(), tasks)
	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}
	for i, r := range results {
		if r.TaskID != tasks[i].ID || r.File != tasks[i].File {
			t.Errorf("result %d: got TaskID=%d File=%q, want TaskID=%d File=%q", i, r.TaskID, r.File, tasks[i].ID, tasks[i].File)
		}
	}
}

func TestPoolRunStreamEmpty(t *testing.T) {
	executor := func(ctx context.Context, task Task) Result {
		t.Fatal("executor should not be called for empty tasks")
		return Result{}
	}
	pool := NewPool(2, executor)
	ch := pool.RunStream(context.Background(), nil)

	var count int
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 results from empty stream, got %d", count)
	}
}

func TestPoolRateLimitSpacesTasks(t *testing.T) {
	var mu sync.Mutex
	var timestamps []time.Time
	tasks := make([]Task, 3)
	for i := range tasks {
		tasks[i] = Task{ID: i, File: fmt.Sprintf("%d.go", i)}
	}
	executor := func(ctx context.Context, task Task) Result {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		return Result{TaskID: task.ID, Success: true}
	}
	pool := NewPoolWithRateLimit(3, 50*time.Millisecond, executor)
	pool.Run(context.Background(), tasks)

	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}
	// With 50ms rate limit between dispatches, total span should be >= 100ms
	span := timestamps[len(timestamps)-1].Sub(timestamps[0])
	if span < 80*time.Millisecond {
		t.Errorf("expected dispatches spaced by rate limit, total span was %s", span)
	}
}

func TestPoolRateLimitRespectsContextCancelRun(t *testing.T) {
	tasks := make([]Task, 3)
	for i := range tasks {
		tasks[i] = Task{ID: i, File: fmt.Sprintf("%d.go", i)}
	}
	executor := func(ctx context.Context, task Task) Result {
		return Result{TaskID: task.ID, Success: true}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	pool := NewPoolWithRateLimit(3, 5*time.Second, executor)
	// Should return quickly despite long rate limit because context is cancelled
	pool.Run(ctx, tasks)
}

func TestPoolRateLimitRespectsContextCancelStream(t *testing.T) {
	tasks := make([]Task, 3)
	for i := range tasks {
		tasks[i] = Task{ID: i, File: fmt.Sprintf("%d.go", i)}
	}
	executor := func(ctx context.Context, task Task) Result {
		return Result{TaskID: task.ID, Success: true}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	pool := NewPoolWithRateLimit(3, 5*time.Second, executor)
	ch := pool.RunStream(ctx, tasks)
	for range ch {
	}
}

// Cancellation observed at the rate-limit gate must stop dispatching the
// remaining tasks, not burst-dispatch them with a dead context. The first
// task has no gate, so exactly one executor call is expected.
func TestPoolRateLimitCancelStopsDispatchStream(t *testing.T) {
	tasks := make([]Task, 3)
	for i := range tasks {
		tasks[i] = Task{ID: i, File: fmt.Sprintf("%d.go", i)}
	}
	var calls atomic.Int64
	executor := func(ctx context.Context, task Task) Result {
		calls.Add(1)
		return Result{Success: true}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool := NewPoolWithRateLimit(3, 5*time.Second, executor)
	var results int
	for range pool.RunStream(ctx, tasks) {
		results++
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 executor call after cancellation, got %d", got)
	}
	if results != 1 {
		t.Errorf("expected 1 result after cancellation, got %d", results)
	}
}

func TestPoolRateLimitCancelStopsDispatchRun(t *testing.T) {
	tasks := make([]Task, 3)
	for i := range tasks {
		tasks[i] = Task{ID: i, File: fmt.Sprintf("%d.go", i)}
	}
	var calls atomic.Int64
	executor := func(ctx context.Context, task Task) Result {
		calls.Add(1)
		return Result{Success: true}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool := NewPoolWithRateLimit(3, 5*time.Second, executor)
	results := pool.Run(ctx, tasks)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 executor call after cancellation, got %d", got)
	}
	if len(results) != 1 {
		t.Errorf("expected results only for dispatched tasks, got %d", len(results))
	}
}

func TestPoolRunEmpty(t *testing.T) {
	executor := func(ctx context.Context, task Task) Result {
		t.Fatal("executor should not be called for empty tasks")
		return Result{}
	}
	pool := NewPool(2, executor)
	results := pool.Run(context.Background(), nil)
	if results != nil {
		t.Errorf("expected nil results for empty tasks, got %v", results)
	}
}
