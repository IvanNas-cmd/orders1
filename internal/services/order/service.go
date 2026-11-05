// Package order — сервисный слой заказов: сценарии поверх домена и хранилища.
//
// Здесь решается, что вообще происходит: провалидировать, посчитать сумму,
// сходить в хранилище, вернуть доменную ошибку. Контроллер после этого
// занимается только переводом в HTTP.
package order

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	domain "github.com/gulmix/orders/internal/models/order"
)

type OrderRepository interface {
	Save(o domain.Order)
	GetByID(id string) (domain.Order, error)
}

type IDGenerator func() string

func defaultIDGenerator() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ord_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("ord_%s", hex.EncodeToString(b[:]))
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

func (s *Service) Create(customerID string, items []domain.Item) (domain.Order, error) {
	id := s.idGen()
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
