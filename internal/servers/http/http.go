// Package http поднимает HTTP-сервер и корректно его останавливает.
package http

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"
)

// Server — HTTP-сервер сервиса вместе с его жизненным циклом.
type Server struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

// NewServer собирает сервер с обязательными таймаутами и готовым *tls.Config.
func NewServer(
	addr string,
	handler http.Handler,
	logger *slog.Logger,
	shutdownTimeout time.Duration,
	tlsConfig *tls.Config,
) *Server {
	return &Server{
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			TLSConfig:         tlsConfig,
		},
		logger:          logger,
		shutdownTimeout: shutdownTimeout,
	}
}

// Serve запускает сервер (с TLS или без).
func (s *Server) Serve() error {
	if s.server.TLSConfig != nil {
		s.logger.Info("HTTP-сервер запущен с TLS",
			slog.String("addr", s.server.Addr),
			slog.Bool("mtls", s.server.TLSConfig.ClientAuth == tls.RequireAndVerifyClientCert),
		)
		return s.server.ListenAndServeTLS("", "")
	}

	s.logger.Info("HTTP-сервер запущен", slog.String("addr", s.server.Addr))
	return s.server.ListenAndServe()
}

// Shutdown корректно останавливает HTTP-сервер.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("остановка HTTP-сервера...")
	return s.server.Shutdown(ctx)
}
