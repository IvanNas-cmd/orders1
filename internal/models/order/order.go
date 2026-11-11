// Package order — доменная модель заказа.
//
// Здесь живут типы предметной области и правила, которые к ним относятся.
// Пакет ничего не знает про HTTP, JSON и базу данных: он не импортирует
// net/http и не содержит тегов сериализации. Это проверяется на приёмке.
package order

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type Status string

const (
	StatusCreated Status = "created"
)

var (
	ErrNotFound   = errors.New("order not found")
	ErrValidation = errors.New("validation error")

	ErrEmptyCustomer   = fmt.Errorf("%w: customer_id cannot be empty", ErrValidation)
	ErrNoItems         = fmt.Errorf("%w: order must contain at least one item", ErrValidation)
	ErrInvalidQuantity = fmt.Errorf("%w: item quantity must be greater than zero", ErrValidation)
	ErrInvalidPrice    = fmt.Errorf("%w: price cannot be negative", ErrValidation)
	ErrInvalidSKU      = fmt.Errorf("%w: sku cannot be empty", ErrValidation)
	ErrMissingQuantity = fmt.Errorf("%w: item quantity is required", ErrValidation)
	ErrMissingPrice    = fmt.Errorf("%w: price_cents is required", ErrValidation)
	ErrOverflow        = fmt.Errorf("%w: order total exceeds maximum value", ErrValidation)
)

type Item struct {
	SKU        string
	Qty        int
	PriceCents int64
}

type Order struct {
	ID         string
	CustomerID string
	Status     Status
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

func NewOrder(id, customerID string, items []Item, createdAt time.Time) (*Order, error) {
	if customerID == "" {
		return nil, ErrEmptyCustomer
	}
	if len(items) == 0 {
		return nil, ErrNoItems
	}

	copiedItems := make([]Item, len(items))
	var total int64
	for i, item := range items {
		if item.SKU == "" {
			return nil, ErrInvalidSKU
		}
		if item.Qty <= 0 {
			return nil, ErrInvalidQuantity
		}
		if item.PriceCents < 0 {
			return nil, ErrInvalidPrice
		}

		if item.PriceCents > 0 && int64(item.Qty) > math.MaxInt64/item.PriceCents {
			return nil, ErrOverflow
		}
		itemTotal := int64(item.Qty) * item.PriceCents

		if math.MaxInt64-total < itemTotal {
			return nil, ErrOverflow
		}
		total += itemTotal

		copiedItems[i] = item
	}

	return &Order{
		ID:         id,
		CustomerID: customerID,
		Status:     StatusCreated,
		Items:      copiedItems,
		TotalCents: total,
		CreatedAt:  createdAt,
	}, nil
}
