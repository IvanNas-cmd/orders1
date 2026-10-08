// Package order — сервисный слой заказов: сценарии поверх домена и хранилища.
//
// Здесь решается, что вообще происходит: провалидировать, посчитать сумму,
// сходить в хранилище, вернуть доменную ошибку. Контроллер после этого
// занимается только переводом в HTTP.
package order

import (
	domain "github.com/gulmix/orders/internal/models/order"
)

type OrderRepository interface {
	Save(o domain.Order)
	GetByID(id string) (domain.Order, error)
}

type Service struct {
	repo OrderRepository
}

func NewService(repo OrderRepository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) Create(id, customerID string, items []domain.Item) (domain.Order, error) {
	order, err := domain.NewOrder(id, customerID, items)
	if err != nil {
		return domain.Order{}, err
	}

	s.repo.Save(*order)
	return *order, nil
}

func (s *Service) GetByID(id string) (domain.Order, error) {
	return s.repo.GetByID(id)
}
