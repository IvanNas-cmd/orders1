package internal_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gulmix/orders/internal"
	healthController "github.com/gulmix/orders/internal/controllers/http/health"
	orderController "github.com/gulmix/orders/internal/controllers/http/order"
	orderRepo "github.com/gulmix/orders/internal/repositories/order"
	orderService "github.com/gulmix/orders/internal/services/order"
)

func TestInitRoutes(t *testing.T) {
	// 1. Создаем логгер для тестов
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// 2. Создаем контроллер /healthz
	health := healthController.NewController()

	// 3. Собираем наши слои заказов (как мы делали в main.go)
	repo := orderRepo.NewRepository()
	service := orderService.NewService(repo)
	orderCtrl := orderController.NewController(service)

	// 4. Передаем все контроллеры в роутер
	handler := internal.InitRoutes(logger, health, orderCtrl)

	// Делаем тестовый HTTP-запрос к /healthz, чтобы убедиться, что роутер работает
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
