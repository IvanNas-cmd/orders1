// Package order — сервисный слой заказов: сценарии поверх домена и хранилища.
//
// Здесь решается, что вообще происходит: провалидировать, посчитать сумму,
// сходить в хранилище, вернуть доменную ошибку. Контроллер после этого
// занимается только переводом в HTTP.
package order

import (
	"context"
	"crypto/rand"
	"time"

	domain "github.com/gulmix/orders/internal/models/order"
)

type OrderRepository interface {
	Save(ctx context.Context, o domain.Order) error
	GetByID(ctx context.Context, id string) (domain.Order, error)
}

type IDGenerator func() string

func defaultIDGenerator() string {
	return "ord_" + rand.Text()
}

type Service struct {
	repo  OrderRepository
	idGen IDGenerator
}

func NewService(repo OrderRepository) *Service {
	return &Service{
		repo:  repo,
		idGen: defaultIDGenerator,
	}
}

// SetIDGenerator позволяет переопределить генератор идентификаторов (например, в тестах).
func (s *Service) SetIDGenerator(gen IDGenerator) {
	s.idGen = gen
}

func (s *Service) Create(ctx context.Context, customerID string, items []domain.Item) (domain.Order, error) {
	id := s.idGen()
	order, err := domain.NewOrder(id, customerID, items, time.Now().UTC())
	if err != nil {
		return domain.Order{}, err
	}

	if err := s.repo.Save(ctx, *order); err != nil {
		return domain.Order{}, err
	}
	return *order, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (domain.Order, error) {
	return s.repo.GetByID(ctx, id)
}
