package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gulmix/orders/internal"
	"github.com/gulmix/orders/internal/config"
	orderGRPCController "github.com/gulmix/orders/internal/controllers/grpc/order"
	healthController "github.com/gulmix/orders/internal/controllers/http/health"
	orderHTTPController "github.com/gulmix/orders/internal/controllers/http/order"
	orderRepo "github.com/gulmix/orders/internal/repositories/order"
	grpcServer "github.com/gulmix/orders/internal/servers/grpc"
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

	tlsConfig, err := config.BuildTLSConfig(cfg)
	if err != nil {
		return err
	}

	// --- Composition Root ---
	// 1. Хранилище заказов (in-memory)
	repo := orderRepo.NewRepository()

	// 2. Сервисный слой с единой бизнес-логикой для HTTP и gRPC
	service := orderService.NewService(repo)

	// 3. HTTP и gRPC контроллеры
	healthCtrl := healthController.NewController()
	orderHTTPCtrl := orderHTTPController.NewController(service)
	orderGRPCCtrl := orderGRPCController.NewController(service)

	// 4. HTTP-сервер
	httpHandler := internal.InitRoutes(logger, healthCtrl, orderHTTPCtrl)
	httpSrv := httpServer.NewServer(cfg.HTTPAddr, httpHandler, logger, cfg.ShutdownTimeout, tlsConfig)

	// 5. gRPC-сервер с reflection и TLS/mTLS
	grpcSrv, err := grpcServer.NewServer(cfg.GRPCAddr, orderGRPCCtrl, logger, cfg.ShutdownTimeout, tlsConfig)
	if err != nil {
		return err
	}

	// 6. Запуск обоих серверов с отслеживанием системных сигналов
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 2)

	go func() {
		if err := httpSrv.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP сервер: %w", err)
		}
	}()

	go func() {
		if err := grpcSrv.Serve(); err != nil {
			errCh <- fmt.Errorf("gRPC сервер: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		stop()
		logger.Info("получен сигнал остановки, завершаем работу")
	}

	// 7. Корректная остановка обоих серверов
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	httpErr := httpSrv.Shutdown(shutdownCtx)
	grpcSrv.Stop()

	if httpErr != nil {
		return fmt.Errorf("остановка HTTP: %w", httpErr)
	}

	logger.Info("все серверы успешно остановлены")
	return nil
}
