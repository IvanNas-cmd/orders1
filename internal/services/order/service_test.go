package order_test

import (
	"context"
	"errors"
	"testing"

	domain "github.com/gulmix/orders/internal/models/order"
	service "github.com/gulmix/orders/internal/services/order"
)

type mockRepo struct {
	saved   map[string]domain.Order
	saveErr error
	getErr  error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		saved: make(map[string]domain.Order),
	}
}

func (m *mockRepo) Save(_ context.Context, o domain.Order) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved[o.ID] = o
	return nil
}

func (m *mockRepo) GetByID(_ context.Context, id string) (domain.Order, error) {
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
	ctx := context.Background()
	repo := newMockRepo()
	svc := service.NewService(repo)
	svc.SetIDGenerator(func() string {
		return "ord_test_123"
	})

	items := []domain.Item{
		{SKU: "book", Qty: 2, PriceCents: 1500},
	}

	ord, err := svc.Create(ctx, "cust_1", items)
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
	ctx := context.Background()
	repo := newMockRepo()
	svc := service.NewService(repo)

	// Ошибка валидации: пустой customer_id
	_, err := svc.Create(ctx, "", []domain.Item{{SKU: "book", Qty: 1, PriceCents: 100}})
	if !errors.Is(err, domain.ErrEmptyCustomer) {
		t.Fatalf("expected ErrEmptyCustomer, got: %v", err)
	}

	if len(repo.saved) != 0 {
		t.Error("repository should not contain any orders on validation error")
	}
}

func TestService_Create_RepoSaveError(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	repo.saveErr = errors.New("db disk full")
	svc := service.NewService(repo)

	items := []domain.Item{
		{SKU: "book", Qty: 1, PriceCents: 100},
	}
	_, err := svc.Create(ctx, "cust_1", items)
	if !errors.Is(err, repo.saveErr) {
		t.Fatalf("expected repo error %v, got: %v", repo.saveErr, err)
	}
}

func TestService_GetByID(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	svc := service.NewService(repo)

	ord := domain.Order{
		ID:         "ord_existing",
		CustomerID: "cust_1",
		Status:     domain.StatusCreated,
		TotalCents: 1000,
	}
	if err := repo.Save(ctx, ord); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	got, err := svc.GetByID(ctx, "ord_existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "ord_existing" {
		t.Errorf("got ID = %q, want 'ord_existing'", got.ID)
	}
}

func TestService_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	svc := service.NewService(repo)

	_, err := svc.GetByID(ctx, "ord_missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
