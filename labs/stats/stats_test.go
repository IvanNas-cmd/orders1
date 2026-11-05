package stats_test

import (
	"sync"
	"testing"

	"github.com/gulmix/orders/labs/stats"
)

func TestStats_Concurrent(t *testing.T) {
	s := stats.New()

	const (
		goroutines = 50
		iterations = 100
	)

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Пишущие горутины
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			status := "created"
			if id%2 == 0 {
				status = "completed"
			}
			for j := 0; j < iterations; j++ {
				s.Record(status)
			}
		}(i)
	}

	// Читающие горутины
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = s.Total()
				_ = s.ByStatus("created")
			}
		}()
	}

	wg.Wait()

	expectedTotal := int64(goroutines * iterations)
	if s.Total() != expectedTotal {
		t.Errorf("Total() = %d, want %d", s.Total(), expectedTotal)
	}
}
