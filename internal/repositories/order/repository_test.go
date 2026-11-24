package order_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	domain "github.com/gulmix/orders/internal/models/order"
	repo "github.com/gulmix/orders/internal/repositories/order"
)

func TestRepository_SaveAndGetByID(t *testing.T) {
	r := repo.NewRepository()

	items := []domain.Item{
		{SKU: "book", Qty: 2, PriceCents: 1500},
	}
	ord := domain.Order{
		ID:         "ord_1",
		CustomerID: "cust_1",
		Status:     "created",
		Items:      items,
		TotalCents: 3000,
		CreatedAt:  time.Now().UTC(),
	}

	r.Save(ord)

	got, err := r.GetByID("ord_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != ord.ID {
		t.Errorf("got ID = %q, want %q", got.ID, ord.ID)
	}
	if got.CustomerID != ord.CustomerID {
		t.Errorf("got CustomerID = %q, want %q", got.CustomerID, ord.CustomerID)
	}
	if got.TotalCents != ord.TotalCents {
		t.Errorf("got TotalCents = %d, want %d", got.TotalCents, ord.TotalCents)
	}
	if len(got.Items) != 1 || got.Items[0].SKU != "book" {
		t.Errorf("got Items = %+v, want %+v", got.Items, ord.Items)
	}
}

func TestRepository_GetByID_NotFound(t *testing.T) {
	r := repo.NewRepository()

	_, err := r.GetByID("non_existent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected domain.ErrNotFound, got: %v", err)
	}
	if !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("expected repo.ErrNotFound, got: %v", err)
	}
}

func TestRepository_Immutability(t *testing.T) {
	r := repo.NewRepository()

	items := []domain.Item{
		{SKU: "book", Qty: 2, PriceCents: 1500},
	}
	ord := domain.Order{
		ID:         "ord_1",
		CustomerID: "cust_1",
		Status:     "created",
		Items:      items,
		TotalCents: 3000,
	}

	r.Save(ord)

	// Модифицируем исходный слайс после сохранения
	items[0].SKU = "hacked_sku"

	got, err := r.GetByID("ord_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Items[0].SKU != "book" {
		t.Errorf("Save() не защитил внутреннее состояние от мутации исходного среза: got SKU %q", got.Items[0].SKU)
	}

	// Модифицируем слайс полученного заказа
	got.Items[0].SKU = "mutated_after_get"

	gotAgain, err := r.GetByID("ord_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAgain.Items[0].SKU != "book" {
		t.Errorf("GetByID() вернул прямую ссылку на внутренний срез: got SKU %q", gotAgain.Items[0].SKU)
	}
}

func TestRepository_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := repo.NewRepository()
	const numGoroutines = 50
	const opsPerGoroutine = 40

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Пишущие горутины
	for i := 0; i < numGoroutines; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				id := fmt.Sprintf("ord_%d_%d", workerID, j)
				ord := domain.Order{
					ID:         id,
					CustomerID: fmt.Sprintf("cust_%d", workerID),
					Status:     "created",
					Items: []domain.Item{
						{SKU: "item", Qty: j + 1, PriceCents: 100},
					},
					TotalCents: int64(j+1) * 100,
					CreatedAt:  time.Now().UTC(),
				}
				r.Save(ord)
			}
		}(i)
	}

	// Читающие горутины
	for i := 0; i < numGoroutines; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				id := fmt.Sprintf("ord_%d_%d", workerID, j)
				ord, err := r.GetByID(id)
				if err == nil {
					if ord.ID != id {
						t.Errorf("got ID %q, want %q", ord.ID, id)
					}
					// Пытаемся мутировать полученный объект
					if len(ord.Items) > 0 {
						ord.Items[0].Qty = 9999
					}
				}
			}
		}(i)
	}

	wg.Wait()
}
