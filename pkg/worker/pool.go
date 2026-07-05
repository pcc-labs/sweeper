package worker

import (
	"context"
	"sync"
	"time"
)

type Executor func(ctx context.Context, task Task) Result

type Pool struct {
	maxWorkers int
	rateLimit  time.Duration
	executor   Executor
}

func NewPool(maxWorkers int, executor Executor) *Pool {
	return &Pool{maxWorkers: maxWorkers, executor: executor}
}

func NewPoolWithRateLimit(maxWorkers int, rateLimit time.Duration, executor Executor) *Pool {
	return &Pool{maxWorkers: maxWorkers, rateLimit: rateLimit, executor: executor}
}

func (p *Pool) RunStream(ctx context.Context, tasks []Task) <-chan Result {
	ch := make(chan Result, len(tasks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.maxWorkers)
	for i, task := range tasks {
		if i > 0 && p.rateLimit > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(p.rateLimit):
			}
			// A bare break here would only exit the select; check the
			// context to stop dispatching the remaining tasks.
			if ctx.Err() != nil {
				break
			}
		}
		wg.Add(1)
		go func(t Task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- stamp(p.executor(ctx, t), t)
		}(task)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}

// stamp sets attribution fields from the task, overwriting whatever the
// executor returned: results stream back in completion order, and consumers
// key per-task state off TaskID, so attribution must not depend on every
// executor remembering to set it.
func stamp(r Result, t Task) Result {
	r.TaskID = t.ID
	r.File = t.File
	return r
}

func (p *Pool) Run(ctx context.Context, tasks []Task) []Result {
	if len(tasks) == 0 {
		return nil
	}
	results := make([]Result, len(tasks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.maxWorkers)
	dispatched := len(tasks)
	for i, task := range tasks {
		if i > 0 && p.rateLimit > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(p.rateLimit):
			}
			// A bare break here would only exit the select; check the
			// context to stop dispatching the remaining tasks.
			if ctx.Err() != nil {
				dispatched = i
				break
			}
		}
		wg.Add(1)
		go func(idx int, t Task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = stamp(p.executor(ctx, t), t)
		}(i, task)
	}
	wg.Wait()
	// Only dispatched tasks have results; never return zero-value entries
	// for tasks that were skipped after cancellation.
	return results[:dispatched]
}
