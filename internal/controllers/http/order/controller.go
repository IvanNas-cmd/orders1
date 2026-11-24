package order

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/gulmix/orders/internal/controllers/http/response"
	domain "github.com/gulmix/orders/internal/models/order"
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
	Create(customerID string, items []domain.Item) (domain.Order, error)
	GetByID(id string) (domain.Order, error)
}

// Controller принимает HTTP-запросы и вызывает сервис
type Controller struct {
	service OrderService
}

func NewController(s OrderService) *Controller {
	return &Controller{service: s}
}

func (c *Controller) Create(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)
	if mediaType != "application/json" {
		response.Error(w, r, http.StatusBadRequest, response.CodeBadRequest, "Content-Type must be application/json")
		return
	}

	var req CreateRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, response.CodeBadRequest, "invalid request body")
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

	order, err := c.service.Create(req.CustomerID, items)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	w.Header().Set("Location", "/orders/"+order.ID)
	response.JSON(w, r, http.StatusCreated, toDTO(order))
}

func (c *Controller) GetByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	order, err := c.service.GetByID(id)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, toDTO(order))
}

func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		response.Error(w, r, http.StatusNotFound, response.CodeNotFound, "order not found")
	case errors.Is(err, domain.ErrEmptyCustomer),
		errors.Is(err, domain.ErrNoItems),
		errors.Is(err, domain.ErrInvalidQuantity),
		errors.Is(err, domain.ErrInvalidPrice):
		response.Error(w, r, http.StatusUnprocessableEntity, response.CodeValidation, err.Error())
	default:
		slog.ErrorContext(r.Context(), "необработанная ошибка", slog.Any("error", err))
		response.Error(w, r, http.StatusInternalServerError, response.CodeInternal, "internal server error")
	}
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
