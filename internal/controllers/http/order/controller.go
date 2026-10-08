package order

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	// Хелперы из шаблона
	"github.com/gulmix/orders/internal/controllers/http/response"
	domain "github.com/gulmix/orders/internal/models/order"
	repo "github.com/gulmix/orders/internal/repositories/order"
)

// ItemDTO описывает позицию в формате JSON
type ItemDTO struct {
	SKU        string `json:"sku"`
	Qty        int    `json:"qty"`
	PriceCents int64  `json:"price_cents"`
}

// CreateRequest описывает тело POST /orders
type CreateRequest struct {
	CustomerID string    `json:"customer_id"`
	Items      []ItemDTO `json:"items"`
}

// OrderResponse описывает то, что мы возвращаем клиенту
type OrderResponse struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	Status     string    `json:"status"`
	Items      []ItemDTO `json:"items"`
	TotalCents int64     `json:"total_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

// OrderService — интерфейс нашего сервисного слоя
type OrderService interface {
	Create(id, customerID string, items []domain.Item) (domain.Order, error)
	GetByID(id string) (domain.Order, error)
}

// Controller принимает HTTP-запросы и дергает сервис
type Controller struct {
	service OrderService
}

func NewController(s OrderService) *Controller {
	return &Controller{service: s}
}

func (c *Controller) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		// Используем response.Error как функцию, передавая w и r
		response.Error(w, r, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	items := make([]domain.Item, len(req.Items))
	for i, item := range req.Items {
		items[i] = domain.Item{
			SKU:        item.SKU,
			Qty:        item.Qty,
			PriceCents: item.PriceCents,
		}
	}

	id := fmt.Sprintf("ord_%d", time.Now().UnixNano())

	order, err := c.service.Create(id, req.CustomerID, items)
	if err != nil {
		// Ошибки бизнес-правил отдаем как 422 Unprocessable Entity
		response.Error(w, r, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	w.Header().Set("Location", "/orders/"+order.ID)
	response.JSON(w, r, http.StatusCreated, toDTO(order))
}

func (c *Controller) GetByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	order, err := c.service.GetByID(id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			response.Error(w, r, http.StatusNotFound, "not_found", "order not found")
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "something went wrong")
		return
	}

	response.JSON(w, r, http.StatusOK, toDTO(order))
}

// toDTO — хелпер для перевода доменной модели в ответ для клиента
func toDTO(o domain.Order) OrderResponse {
	items := make([]ItemDTO, len(o.Items))
	for i, item := range o.Items {
		items[i] = ItemDTO{
			SKU:        item.SKU,
			Qty:        item.Qty,
			PriceCents: item.PriceCents,
		}
	}
	return OrderResponse{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     o.Status,
		Items:      items,
		TotalCents: o.TotalCents,
		CreatedAt:  o.CreatedAt,
	}
}
