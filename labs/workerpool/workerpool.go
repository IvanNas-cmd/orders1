// Package workerpool реализует пул воркеров с ограничением параллелизма.
package workerpool

import (
	"context"
	"errors"
	"sync"
)

// Run выполняет переданные задачи jobs, ограничивая число одновременно работающих до n.
// Если ctx отменяется, новые задачи не запускаются. Ошибки задач объединяются через errors.Join.
// К моменту возврата из Run все запущенные горутины гарантированно завершаются.
func Run(ctx context.Context, n int, jobs []func(context.Context) error) error {
	if n <= 0 {
		n = 1
	}
	if len(jobs) == 0 {
		return ctx.Err()
	}

	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, n)
		errMu   sync.Mutex
		errList []error
	)

	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			errMu.Lock()
			errList = append(errList, err)
			errMu.Unlock()
			break
		}

		select {
		case <-ctx.Done():
			errMu.Lock()
			errList = append(errList, ctx.Err())
			errMu.Unlock()
			goto wait
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(fn func(context.Context) error) {
			defer func() {
				<-sem
				wg.Done()
			}()

			if err := fn(ctx); err != nil {
				errMu.Lock()
				errList = append(errList, err)
				errMu.Unlock()
			}
		}(job)
	}

wait:
	wg.Wait()
	return errors.Join(errList...)
}
