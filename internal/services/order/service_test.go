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

func TestService_UpdateStatus(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewService(repo)

	ord := domain.Order{
		ID:         "ord_1",
		CustomerID: "cust_1",
		Status:     domain.StatusCreated,
	}
	repo.Save(ord)

	// Подписываемся на заказ
	ch, unsub := svc.Subscribe("ord_1")
	defer unsub()

	if svc.SubscriberCount("ord_1") != 1 {
		t.Errorf("expected 1 subscriber, got %d", svc.SubscriberCount("ord_1"))
	}

	// Успешный переход created -> paid
	updated, err := svc.UpdateStatus("ord_1", domain.StatusPaid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != domain.StatusPaid {
		t.Errorf("expected status paid, got %s", updated.Status)
	}

	// Получаем событие в канале
	select {
	case event := <-ch:
		if event.Status != domain.StatusPaid {
			t.Errorf("expected event status paid, got %s", event.Status)
		}
	default:
		t.Fatal("expected event in subscriber channel")
	}

	// Недопустимый переход paid -> created
	_, err = svc.UpdateStatus("ord_1", domain.StatusCreated)
	if !errors.Is(err, domain.ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got %v", err)
	}

	// Отписка
	unsub()
	if svc.SubscriberCount("ord_1") != 0 {
		t.Errorf("expected 0 subscribers after unsub, got %d", svc.SubscriberCount("ord_1"))
	}
}

func TestService_SlowSubscriber(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewService(repo)

	ord := domain.Order{
		ID:         "ord_slow",
		CustomerID: "cust_1",
		Status:     domain.StatusCreated,
	}
	repo.Save(ord)

	ch, unsub := svc.Subscribe("ord_slow")
	defer unsub()

	// Выполняем цепочку быстрых обновлений без вычитывания из ch:
	// created -> paid -> shipped -> delivered
	_, err := svc.UpdateStatus("ord_slow", domain.StatusPaid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = svc.UpdateStatus("ord_slow", domain.StatusShipped)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = svc.UpdateStatus("ord_slow", domain.StatusDelivered)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Медленный подписчик читает канал: он гарантированно получает финальный статус
	event := <-ch
	if event.Status != domain.StatusDelivered {
		t.Errorf("expected slow subscriber to receive latest status delivered, got %s", event.Status)
	}
}
