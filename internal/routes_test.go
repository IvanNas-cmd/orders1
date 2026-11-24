package internal_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gulmix/orders/internal"
	healthController "github.com/gulmix/orders/internal/controllers/http/health"
	orderController "github.com/gulmix/orders/internal/controllers/http/order"
	orderRepo "github.com/gulmix/orders/internal/repositories/order"
	"github.com/gulmix/orders/internal/requestid"
	orderService "github.com/gulmix/orders/internal/services/order"
)

func setupTestApp() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	health := healthController.NewController()
	repo := orderRepo.NewRepository()
	service := orderService.NewService(repo)
	orderCtrl := orderController.NewController(service)

	return internal.InitRoutes(logger, health, orderCtrl)
}

func TestHealth(t *testing.T) {
	app := setupTestApp()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, expected %d", rec.Code, http.StatusOK)
	}

	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf(`status = %q, expected "ok"`, got["status"])
	}

	if rec.Header().Get(requestid.Header) == "" {
		t.Errorf("header %s is empty", requestid.Header)
	}
}

func TestOrders_EndToEnd(t *testing.T) {
	app := setupTestApp()

	// 1. Создаем заказ через POST /orders
	postPayload := `{"customer_id": "cust_1", "items": [{"sku": "book", "qty": 2, "price_cents": 1500}]}`
	reqPost := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(postPayload))
	reqPost.Header.Set("Content-Type", "application/json")
	recPost := httptest.NewRecorder()

	app.ServeHTTP(recPost, reqPost)

	if recPost.Code != http.StatusCreated {
		t.Fatalf("POST /orders status = %d, expected %d. Body: %s", http.StatusCreated, recPost.Code, recPost.Body.String())
	}

	location := recPost.Header().Get("Location")
	if location == "" {
		t.Fatal("Location header is missing in 201 response")
	}

	var created orderController.OrderResponse
	if err := json.NewDecoder(recPost.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created order JSON: %v", err)
	}

	if created.ID == "" {
		t.Error("created order ID is empty")
	}
	if location != "/orders/"+created.ID {
		t.Errorf("Location header = %q, expected %q", location, "/orders/"+created.ID)
	}
	if created.CustomerID != "cust_1" {
		t.Errorf("customer_id = %q, expected 'cust_1'", created.CustomerID)
	}
	if created.Status != "created" {
		t.Errorf("status = %q, expected 'created'", created.Status)
	}
	if created.TotalCents != 3000 {
		t.Errorf("total_cents = %d, expected 3000", created.TotalCents)
	}

	// 2. Получаем созданный заказ через GET /orders/{id}
	reqGet := httptest.NewRequest(http.MethodGet, location, nil)
	recGet := httptest.NewRecorder()

	app.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, expected %d. Body: %s", location, http.StatusOK, recGet.Code, recGet.Body.String())
	}

	var fetched orderController.OrderResponse
	if err := json.NewDecoder(recGet.Body).Decode(&fetched); err != nil {
		t.Fatalf("failed to decode fetched order JSON: %v", err)
	}

	if fetched.ID != created.ID {
		t.Errorf("fetched ID = %q, expected %q", fetched.ID, created.ID)
	}
	if fetched.TotalCents != created.TotalCents {
		t.Errorf("fetched TotalCents = %d, expected %d", fetched.TotalCents, created.TotalCents)
	}
	if fetched.CustomerID != created.CustomerID {
		t.Errorf("fetched CustomerID = %q, expected %q", fetched.CustomerID, created.CustomerID)
	}

	// 3. Проверяем 404 для несуществующего заказа
	reqNotFound := httptest.NewRequest(http.MethodGet, "/orders/non_existent_123", nil)
	recNotFound := httptest.NewRecorder()

	app.ServeHTTP(recNotFound, reqNotFound)

	if recNotFound.Code != http.StatusNotFound {
		t.Fatalf("GET /orders/non_existent_123 status = %d, expected %d", recNotFound.Code, http.StatusNotFound)
	}
}
