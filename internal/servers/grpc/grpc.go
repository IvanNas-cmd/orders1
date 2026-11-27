package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	ordersv1 "github.com/gulmix/orders/internal/api/orders/v1"
)

// Server управляет жизненным циклом gRPC-сервера.
type Server struct {
	server          *grpc.Server
	listener        net.Listener
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

// NewServer конфигурирует gRPC-сервер с поддержкой reflection и TLS/mTLS (если tlsConfig != nil).
func NewServer(
	addr string,
	orderServiceServer ordersv1.OrderServiceServer,
	logger *slog.Logger,
	shutdownTimeout time.Duration,
	tlsConfig *tls.Config,
) (*Server, error) {
	var opts []grpc.ServerOption
	if tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	grpcServer := grpc.NewServer(opts...)
	ordersv1.RegisterOrderServiceServer(grpcServer, orderServiceServer)
	reflection.Register(grpcServer)

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return &Server{
		server:          grpcServer,
		listener:        ln,
		logger:          logger,
		shutdownTimeout: shutdownTimeout,
	}, nil
}

// Addr возвращает фактический сетевой адрес слушателя (полезно при `:0` в тестах).
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Serve запускает приём соединений. Блокируется до остановки сервера.
func (s *Server) Serve() error {
	s.logger.Info("gRPC-сервер запущен", slog.String("addr", s.Addr()))
	return s.server.Serve(s.listener)
}

// Stop останавливает gRPC-сервер.
// Сначала пробует GracefulStop. Если активные потоки (например, WatchOrder) не завершаются
// за shutdownTimeout, принудительно вызывает Stop(), чтобы не держать процесс вечно.
func (s *Server) Stop() {
	stopped := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		s.logger.Info("gRPC-сервер остановлен корректно")
	case <-time.After(s.shutdownTimeout):
		s.logger.Warn("gRPC-сервер не остановился вовремя, принудительная остановка активных потоков")
		s.server.Stop()
	}
}
