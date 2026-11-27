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
	"sync"
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

type subscriber struct {
	id int64
	ch chan domain.Order
}

type Service struct {
	repo  OrderRepository
	idGen IDGenerator

	mu     sync.RWMutex
	subSeq int64
	subs   map[string]map[int64]*subscriber
}

func NewService(repo OrderRepository) *Service {
	return &Service{
		repo:  repo,
		idGen: defaultIDGenerator,
		subs:  make(map[string]map[int64]*subscriber),
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

// UpdateStatus обновляет статус существующего заказа и уведомляет подписчиков.
func (s *Service) UpdateStatus(id, newStatus string) (domain.Order, error) {
	ord, err := s.repo.GetByID(id)
	if err != nil {
		return domain.Order{}, err
	}

	if err := ord.UpdateStatus(newStatus); err != nil {
		return domain.Order{}, err
	}

	s.repo.Save(ord)
	s.notifySubscribers(ord)

	return ord, nil
}

// Subscribe подписывает слушателя на обновления статуса заказа orderID.
// Возвращает канал с буфером на 1 элемент (для гарантии доставки последнего статуса без блокировки продюсера)
// и функцию отписки.
func (s *Service) Subscribe(orderID string) (<-chan domain.Order, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.subSeq++
	subID := s.subSeq
	subCh := make(chan domain.Order, 1)
	sub := &subscriber{
		id: subID,
		ch: subCh,
	}

	if s.subs[orderID] == nil {
		s.subs[orderID] = make(map[int64]*subscriber)
	}
	s.subs[orderID][subID] = sub

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if orderSubs, ok := s.subs[orderID]; ok {
				delete(orderSubs, subID)
				if len(orderSubs) == 0 {
					delete(s.subs, orderID)
				}
			}
		})
	}

	return subCh, unsubscribe
}

// SubscriberCount возвращает текущее количество активных подписчиков на заказ orderID.
func (s *Service) SubscriberCount(orderID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs[orderID])
}

// notifySubscribers отправляет заказ всем подписчикам.
// Для медленных подписчиков освобождается старый элемент и кладется новейший статус,
// исключая блокировку UpdateStatus.
func (s *Service) notifySubscribers(ord domain.Order) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orderSubs := s.subs[ord.ID]
	for _, sub := range orderSubs {
		select {
		case sub.ch <- ord:
		default:
			// Канал полон (медленный подписчик): вычитываем старый статус
			select {
			case <-sub.ch:
			default:
			}
			// Кладём самый последний статус
			select {
			case sub.ch <- ord:
			default:
			}
		}
	}
}
