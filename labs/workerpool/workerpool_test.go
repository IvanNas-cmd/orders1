package workerpool_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gulmix/orders/labs/workerpool"
)

func TestRun_ConcurrencyLimit(t *testing.T) {
	const (
		limit   = 3
		jobNum  = 15
		jobTime = 20 * time.Millisecond
	)

	var (
		active         atomic.Int64
		maxConcurrent  atomic.Int64
		completedCount atomic.Int64
	)

	jobs := make([]func(context.Context) error, jobNum)
	for i := range jobs {
		jobs[i] = func(ctx context.Context) error {
			cur := active.Add(1)
			defer active.Add(-1)

			// Фиксируем максимальное количество параллельных задач
			for {
				oldMax := maxConcurrent.Load()
				if cur <= oldMax || maxConcurrent.CompareAndSwap(oldMax, cur) {
					break
				}
			}

			time.Sleep(jobTime)
			completedCount.Add(1)
			return nil
		}
	}

	err := workerpool.Run(context.Background(), limit, jobs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if completedCount.Load() != jobNum {
		t.Errorf("expected %d completed jobs, got %d", jobNum, completedCount.Load())
	}

	if maxConcurrent.Load() > limit {
		t.Errorf("concurrency limit exceeded: max concurrent was %d, limit was %d", maxConcurrent.Load(), limit)
	}

	if active.Load() != 0 {
		t.Errorf("expected 0 active goroutines after Run, got %d", active.Load())
	}
}

func TestRun_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const totalJobs = 20
	const limit = 2

	var (
		startedCount atomic.Int64
		active       atomic.Int64
	)

	jobs := make([]func(context.Context) error, totalJobs)
	for i := range jobs {
		jobs[i] = func(jobCtx context.Context) error {
			started := startedCount.Add(1)
			active.Add(1)
			defer active.Add(-1)

			// Вторая задача отменяет общий контекст
			if started == 2 {
				cancel()
			}

			time.Sleep(20 * time.Millisecond)
			return nil
		}
	}

	err := workerpool.Run(ctx, limit, jobs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in returned error, got: %v", err)
	}

	if startedCount.Load() >= totalJobs {
		t.Errorf("expected some jobs to be canceled, but all %d started", startedCount.Load())
	}

	if active.Load() != 0 {
		t.Errorf("expected 0 active goroutines after Run return, got %d", active.Load())
	}
}

func TestRun_ErrorAggregation(t *testing.T) {
	err1 := errors.New("err 1")
	err2 := errors.New("err 2")

	jobs := []func(context.Context) error{
		func(ctx context.Context) error { return err1 },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return err2 },
	}

	err := workerpool.Run(context.Background(), 2, jobs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, err1) {
		t.Errorf("expected errors.Is(err, err1), got: %v", err)
	}
	if !errors.Is(err, err2) {
		t.Errorf("expected errors.Is(err, err2), got: %v", err)
	}
}

func TestRun_EdgeCases(t *testing.T) {
	t.Run("empty jobs", func(t *testing.T) {
		err := workerpool.Run(context.Background(), 5, nil)
		if err != nil {
			t.Errorf("expected nil for empty jobs, got %v", err)
		}
	})

	t.Run("already canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var executed atomic.Bool
		jobs := []func(context.Context) error{
			func(ctx context.Context) error {
				executed.Store(true)
				return nil
			},
		}

		err := workerpool.Run(ctx, 2, jobs)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if executed.Load() {
			t.Error("job should not execute with already canceled context")
		}
	})
}

func BenchmarkRun(b *testing.B) {
	for _, n := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			// Набор из 64 задач по 1 мс
			jobs := make([]func(context.Context) error, 64)
			for i := range jobs {
				jobs[i] = func(ctx context.Context) error {
					time.Sleep(time.Millisecond)
					return nil
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = workerpool.Run(context.Background(), n, jobs)
			}
		})
	}
}
