package order_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	orderController "github.com/gulmix/orders/internal/controllers/http/order"
	"github.com/gulmix/orders/internal/controllers/http/response"
	domain "github.com/gulmix/orders/internal/models/order"
)

type mockService struct {
	createFunc  func(customerID string, items []domain.Item) (domain.Order, error)
	getByIDFunc func(id string) (domain.Order, error)
}

func (m *mockService) Create(customerID string, items []domain.Item) (domain.Order, error) {
	if m.createFunc != nil {
		return m.createFunc(customerID, items)
	}
	return domain.Order{}, nil
}

func (m *mockService) GetByID(id string) (domain.Order, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(id)
	}
	return domain.Order{}, nil
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func TestController_Create_Success(t *testing.T) {
	mock := &mockService{
		createFunc: func(customerID string, items []domain.Item) (domain.Order, error) {
			return domain.Order{
				ID:         "ord_123",
				CustomerID: customerID,
				Status:     "created",
				Items:      items,
				TotalCents: 3000,
				CreatedAt:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	ctrl := orderController.NewController(mock)

	body := `{"customer_id": "cust_1", "items": [{"sku": "book", "qty": 2, "price_cents": 1500}]}`
	req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ctrl.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if location != "/orders/ord_123" {
		t.Errorf("expected Location '/orders/ord_123', got %q", location)
	}

	var resp orderController.OrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != "ord_123" {
		t.Errorf("got ID = %q, want 'ord_123'", resp.ID)
	}
	if resp.CustomerID != "cust_1" {
		t.Errorf("got CustomerID = %q, want 'cust_1'", resp.CustomerID)
	}
	if resp.Status != "created" {
		t.Errorf("got Status = %q, want 'created'", resp.Status)
	}
	if resp.TotalCents != 3000 {
		t.Errorf("got TotalCents = %d, want 3000", resp.TotalCents)
	}
	if len(resp.Items) != 1 || resp.Items[0].SKU != "book" {
		t.Errorf("unexpected items: %+v", resp.Items)
	}
}

func TestController_Create_BadRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "не тот Content-Type",
			contentType: "text/plain",
			body:        `{"customer_id": "cust_1", "items": [{"sku": "book", "qty": 1, "price_cents": 100}]}`,
		},
		{
			name:        "отсутствует Content-Type",
			contentType: "",
			body:        `{"customer_id": "cust_1", "items": [{"sku": "book", "qty": 1, "price_cents": 100}]}`,
		},
		{
			name:        "битый JSON",
			contentType: "application/json",
			body:        `{"customer_id": "cust_1", "items": [`,
		},
		{
			name:        "неизвестное поле (DisallowUnknownFields)",
			contentType: "application/json",
			body:        `{"customer_id": "cust_1", "unknown_field": 123, "items": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := orderController.NewController(&mockService{})

			req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()

			ctrl.Create(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d. Body: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
			}

			var env errorEnvelope
			if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
				t.Fatalf("failed to decode error body: %v", err)
			}
			if env.Error.Code != response.CodeBadRequest {
				t.Errorf("expected error code %q, got %q", response.CodeBadRequest, env.Error.Code)
			}
		})
	}
}

func TestController_Create_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr string
	}{
		{
			name:    "пустой customer_id",
			err:     domain.ErrEmptyCustomer,
			wantErr: domain.ErrEmptyCustomer.Error(),
		},
		{
			name:    "пустые items",
			err:     domain.ErrNoItems,
			wantErr: domain.ErrNoItems.Error(),
		},
		{
			name:    "некорректное количество",
			err:     domain.ErrInvalidQuantity,
			wantErr: domain.ErrInvalidQuantity.Error(),
		},
		{
			name:    "отрицательная цена",
			err:     domain.ErrInvalidPrice,
			wantErr: domain.ErrInvalidPrice.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{
				createFunc: func(customerID string, items []domain.Item) (domain.Order, error) {
					return domain.Order{}, tt.err
				},
			}
			ctrl := orderController.NewController(mock)

			body := `{"customer_id": "cust_1", "items": [{"sku": "book", "qty": 1, "price_cents": 100}]}`
			req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			ctrl.Create(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected status %d, got %d. Body: %s", http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			}

			var env errorEnvelope
			if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
				t.Fatalf("failed to decode error body: %v", err)
			}
			if env.Error.Code != response.CodeValidation {
				t.Errorf("expected error code %q, got %q", response.CodeValidation, env.Error.Code)
			}
			if env.Error.Message != tt.wantErr {
				t.Errorf("expected error message %q, got %q", tt.wantErr, env.Error.Message)
			}
		})
	}
}

func TestController_GetByID_Success(t *testing.T) {
	mock := &mockService{
		getByIDFunc: func(id string) (domain.Order, error) {
			return domain.Order{
				ID:         id,
				CustomerID: "cust_1",
				Status:     "created",
				Items: []domain.Item{
					{SKU: "book", Qty: 1, PriceCents: 500},
				},
				TotalCents: 500,
				CreatedAt:  time.Now().UTC(),
			}, nil
		},
	}
	ctrl := orderController.NewController(mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", ctrl.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/orders/ord_test", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp orderController.OrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != "ord_test" {
		t.Errorf("got ID = %q, want 'ord_test'", resp.ID)
	}
}

func TestController_GetByID_NotFound(t *testing.T) {
	mock := &mockService{
		getByIDFunc: func(id string) (domain.Order, error) {
			return domain.Order{}, domain.ErrNotFound
		},
	}
	ctrl := orderController.NewController(mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", ctrl.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/orders/ord_missing", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}

	var env errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if env.Error.Code != response.CodeNotFound {
		t.Errorf("expected error code %q, got %q", response.CodeNotFound, env.Error.Code)
	}
}

func TestController_InternalError(t *testing.T) {
	mock := &mockService{
		getByIDFunc: func(id string) (domain.Order, error) {
			return domain.Order{}, errors.New("database connection failed")
		},
	}
	ctrl := orderController.NewController(mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", ctrl.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/orders/ord_err", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var env errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if env.Error.Code != response.CodeInternal {
		t.Errorf("expected error code %q, got %q", response.CodeInternal, env.Error.Code)
	}
}
