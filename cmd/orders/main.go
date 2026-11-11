package main

import (
	"log/slog"
	"os"

	"github.com/gulmix/orders/internal"
	"github.com/gulmix/orders/internal/config"
	healthController "github.com/gulmix/orders/internal/controllers/http/health"

	orderController "github.com/gulmix/orders/internal/controllers/http/order"
	orderRepo "github.com/gulmix/orders/internal/repositories/order"
	httpServer "github.com/gulmix/orders/internal/servers/http"
	orderService "github.com/gulmix/orders/internal/services/order"
)

func main() {
	if err := run(); err != nil {
		slog.Error("сервис остановлен с ошибкой", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	health := healthController.NewController()

	// --- Собираем матрешку заказов (Composition Root) ---
	// 1. Создаем хранилище (самый нижний слой)
	repo := orderRepo.NewRepository()

	// 2. Создаем бизнес-логику и передаем ей хранилище
	service := orderService.NewService(repo)

	// 3. Создаем HTTP-контроллер и передаем ему бизнес-логику
	orderCtrl := orderController.NewController(service)
	// ----------------------------------------------------

	// 4. Передаем оба контроллера (health и orderCtrl) в маршрутизатор
	srv := httpServer.NewServer(cfg.HTTPAddr, internal.InitRoutes(logger, health, orderCtrl), logger, cfg.ShutdownTimeout)

	return srv.Run()
}
