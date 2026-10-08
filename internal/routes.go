package internal

import (
	"log/slog"
	"net/http"

	healthController "github.com/gulmix/orders/internal/controllers/http/health"
	orderController "github.com/gulmix/orders/internal/controllers/http/order" // 1. Импортируем наш контроллер
	"github.com/gulmix/orders/internal/middlewares"
)

// 2. Добавляем orderCtrl в список аргументов функции
func InitRoutes(logger *slog.Logger, health *healthController.Controller, orderCtrl *orderController.Controller) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", health.Health)

	// 3. Регистрируем пути для заказов и привязываем их к методам контроллера
	mux.HandleFunc("POST /orders", orderCtrl.Create)
	mux.HandleFunc("GET /orders/{id}", orderCtrl.GetByID)

	var h http.Handler = mux
	h = middlewares.Recover(logger, h)
	h = middlewares.Logging(logger, h)
	h = middlewares.RequestID(h)

	return h
}
