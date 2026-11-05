// Package order — хранилище заказов.
//
// В Модуле 0 это карта в памяти. В Модуле 5 на её место встанет Postgres,
// и если границы проведены правильно, менять придётся только этот пакет.
package order

import (
	"sync"

	domain "github.com/gulmix/orders/internal/models/order"
)

var ErrNotFound = domain.ErrNotFound

type Repository struct {
	mu     sync.RWMutex
	orders map[string]domain.Order
}

func NewRepository() *Repository {
	return &Repository{
		orders: make(map[string]domain.Order),
	}
}

func (r *Repository) Save(o domain.Order) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[o.ID] = o.Clone()
}

func (r *Repository) GetByID(id string) (domain.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	o, ok := r.orders[id]
	if !ok {
		return domain.Order{}, domain.ErrNotFound
	}
	return o.Clone(), nil
}
