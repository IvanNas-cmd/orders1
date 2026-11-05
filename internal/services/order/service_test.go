package order_test

import (
	"errors"
	"testing"

	domain "github.com/gulmix/orders/internal/models/order"
	service "github.com/gulmix/orders/internal/services/order"
)

type mockRepo struct {
	saved  map[string]domain.Order
	getErr error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		saved: make(map[string]domain.Order),
	}
}

func (m *mockRepo) Save(o domain.Order) {
	m.saved[o.ID] = o
}

func (m *mockRepo) GetByID(id string) (domain.Order, error) {
	if m.getErr != nil {
		return domain.Order{}, m.getErr
	}
	o, ok := m.saved[id]
	if !ok {
		return domain.Order{}, domain.ErrNotFound
	}
	return o, nil
}

func TestService_Create(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewService(repo)
	svc.SetIDGenerator(func() string {
		return "ord_test_123"
	})

	items := []domain.Item{
		{SKU: "book", Qty: 2, PriceCents: 1500},
	}

	ord, err := svc.Create("cust_1", items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ord.ID != "ord_test_123" {
		t.Errorf("got ID = %q, want 'ord_test_123'", ord.ID)
	}
	if ord.TotalCents != 3000 {
		t.Errorf("got TotalCents = %d, want 3000", ord.TotalCents)
	}

	// Проверяем, что заказ сохранён в репозитории
	saved, ok := repo.saved["ord_test_123"]
	if !ok {
		t.Fatal("order was not saved in repository")
	}
	if saved.ID != "ord_test_123" {
		t.Errorf("saved ID = %q, want 'ord_test_123'", saved.ID)
	}
}

func TestService_Create_ValidationError(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewService(repo)

	// Ошибка валидации: пустой customer_id
	_, err := svc.Create("", []domain.Item{{SKU: "book", Qty: 1, PriceCents: 100}})
	if !errors.Is(err, domain.ErrEmptyCustomer) {
		t.Fatalf("expected ErrEmptyCustomer, got: %v", err)
	}

	if len(repo.saved) != 0 {
		t.Error("repository should not contain any orders on validation error")
	}
}

func TestService_GetByID(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewService(repo)

	ord := domain.Order{
		ID:         "ord_existing",
		CustomerID: "cust_1",
		Status:     "created",
		TotalCents: 1000,
	}
	repo.Save(ord)

	got, err := svc.GetByID("ord_existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "ord_existing" {
		t.Errorf("got ID = %q, want 'ord_existing'", got.ID)
	}

	_, err = svc.GetByID("ord_non_existing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
