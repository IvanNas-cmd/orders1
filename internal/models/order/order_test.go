package order_test

import (
	"errors"
	"testing"

	domain "github.com/gulmix/orders/internal/models/order"
)

func TestNewOrder(t *testing.T) {
	validItems := []domain.Item{
		{SKU: "book", Qty: 2, PriceCents: 1500},
		{SKU: "pen", Qty: 1, PriceCents: 200},
	}

	tests := []struct {
		name       string
		id         string
		customerID string
		items      []domain.Item
		wantErr    error
		wantTotal  int64
	}{
		{
			name:       "успешное создание заказа",
			id:         "ord_1",
			customerID: "cust_1",
			items:      validItems,
			wantErr:    nil,
			wantTotal:  3200,
		},
		{
			name:       "пустой customer_id",
			id:         "ord_1",
			customerID: "",
			items:      validItems,
			wantErr:    domain.ErrEmptyCustomer,
		},
		{
			name:       "пустой список позиций",
			id:         "ord_1",
			customerID: "cust_1",
			items:      []domain.Item{},
			wantErr:    domain.ErrNoItems,
		},
		{
			name:       "nil список позиций",
			id:         "ord_1",
			customerID: "cust_1",
			items:      nil,
			wantErr:    domain.ErrNoItems,
		},
		{
			name:       "qty равно 0",
			id:         "ord_1",
			customerID: "cust_1",
			items:      []domain.Item{{SKU: "book", Qty: 0, PriceCents: 100}},
			wantErr:    domain.ErrInvalidQuantity,
		},
		{
			name:       "qty меньше 0",
			id:         "ord_1",
			customerID: "cust_1",
			items:      []domain.Item{{SKU: "book", Qty: -1, PriceCents: 100}},
			wantErr:    domain.ErrInvalidQuantity,
		},
		{
			name:       "отрицательная цена",
			id:         "ord_1",
			customerID: "cust_1",
			items:      []domain.Item{{SKU: "book", Qty: 1, PriceCents: -50}},
			wantErr:    domain.ErrInvalidPrice,
		},
		{
			name:       "цена равна 0 допустима",
			id:         "ord_1",
			customerID: "cust_1",
			items:      []domain.Item{{SKU: "free_gift", Qty: 1, PriceCents: 0}},
			wantErr:    nil,
			wantTotal:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NewOrder(tt.id, tt.customerID, tt.items)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewOrder() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			if got.ID != tt.id {
				t.Errorf("got ID = %q, want %q", got.ID, tt.id)
			}
			if got.CustomerID != tt.customerID {
				t.Errorf("got CustomerID = %q, want %q", got.CustomerID, tt.customerID)
			}
			if got.Status != "created" {
				t.Errorf("got Status = %q, want 'created'", got.Status)
			}
			if got.TotalCents != tt.wantTotal {
				t.Errorf("got TotalCents = %d, want %d", got.TotalCents, tt.wantTotal)
			}
			if len(got.Items) != len(tt.items) {
				t.Fatalf("got len(Items) = %d, want %d", len(got.Items), len(tt.items))
			}
		})
	}
}

func TestOrder_Clone(t *testing.T) {
	items := []domain.Item{
		{SKU: "item1", Qty: 1, PriceCents: 100},
	}
	ord, err := domain.NewOrder("ord_1", "cust_1", items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cloned := ord.Clone()
	cloned.Items[0].SKU = "mutated"

	if ord.Items[0].SKU == "mutated" {
		t.Error("Clone() не создал независимый срез Items")
	}
}
