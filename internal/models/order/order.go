// Package order — доменная модель заказа.
//
// Здесь живут типы предметной области и правила, которые к ним относятся.
// Пакет ничего не знает про HTTP, JSON и базу данных: он не импортирует
// net/http и не содержит тегов сериализации. Это проверяется на приёмке.
package order

import (
	"errors"
	"time"
)

var (
	ErrNotFound        = errors.New("order not found")
	ErrEmptyCustomer   = errors.New("customer_id cannot be empty")
	ErrNoItems         = errors.New("order must contain at least one item")
	ErrInvalidQuantity = errors.New("item quantity must be greater than zero")
	ErrInvalidPrice    = errors.New("price cannot be negative")
)

type Item struct {
	SKU        string
	Qty        int
	PriceCents int64
}

type Order struct {
	ID         string
	CustomerID string
	Status     string
	Items      []Item
	TotalCents int64
	CreatedAt  time.Time
}

// Clone возвращает копию заказа с независимым срезом позиций.
func (o Order) Clone() Order {
	if o.Items == nil {
		return o
	}
	items := make([]Item, len(o.Items))
	copy(items, o.Items)
	o.Items = items
	return o
}

func NewOrder(id, customerID string, items []Item) (*Order, error) {
	if customerID == "" {
		return nil, ErrEmptyCustomer
	}
	if len(items) == 0 {
		return nil, ErrNoItems
	}

	copiedItems := make([]Item, len(items))
	var total int64
	for i, item := range items {
		if item.Qty <= 0 {
			return nil, ErrInvalidQuantity
		}
		if item.PriceCents < 0 {
			return nil, ErrInvalidPrice
		}

		total += int64(item.Qty) * item.PriceCents
		copiedItems[i] = item
	}

	return &Order{
		ID:         id,
		CustomerID: customerID,
		Status:     "created",
		Items:      copiedItems,
		TotalCents: total,
		CreatedAt:  time.Now().UTC(),
	}, nil
}
