// Package config собирает конфигурацию сервиса из переменных окружения.
//
// Правило курса: никаких флагов и файлов настроек в коде сервиса — только
// окружение (12-factor). Дальше по курсу сюда добавятся DSN базы (Модуль 5),
// адрес брокера (Модуль 8) и параметры таймаутов (Модуль 12).
package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

var (
	ErrTLSPair            = errors.New("ORDERS_TLS_CERT_FILE и ORDERS_TLS_KEY_FILE должны быть заданы вместе")
	ErrClientCAWithoutTLS = errors.New("ORDERS_TLS_CLIENT_CA_FILE требует, чтобы были заданы ORDERS_TLS_CERT_FILE и ORDERS_TLS_KEY_FILE")
)

// Config — все настройки сервиса в одном месте.
type Config struct {
	// HTTPAddr — адрес, который слушает HTTP-сервер, например ":8080".
	HTTPAddr string
	// GRPCAddr — адрес, который слушает gRPC-сервер, например ":9090".
	GRPCAddr string
	// ShutdownTimeout — сколько ждём завершения активных запросов при остановке.
	ShutdownTimeout time.Duration
	// LogLevel — уровень логирования.
	LogLevel slog.Level
	// TLSCertFile — путь к файлу TLS-сертификата.
	TLSCertFile string
	// TLSKeyFile — путь к файлу приватного ключа TLS.
	TLSKeyFile string
	// TLSClientCAFile — путь к сертификату CA для проверки клиентов (mTLS).
	TLSClientCAFile string
}

// Load читает конфигурацию из окружения, подставляя значения по умолчанию.
// Возвращает ошибку, если значение задано, но некорректно: сервис должен
// падать на старте, а не работать с непонятной конфигурацией.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        env("ORDERS_HTTP_ADDR", ":8080"),
		GRPCAddr:        env("ORDERS_GRPC_ADDR", ":9090"),
		ShutdownTimeout: 10 * time.Second,
		LogLevel:        slog.LevelInfo,
	}

	if raw, ok := os.LookupEnv("ORDERS_SHUTDOWN_TIMEOUT"); ok {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("ORDERS_SHUTDOWN_TIMEOUT=%q: %w", raw, err)
		}
		cfg.ShutdownTimeout = d
	}

	if raw, ok := os.LookupEnv("ORDERS_LOG_LEVEL"); ok {
		lvl, err := parseLevel(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.LogLevel = lvl
	}

	cfg.TLSCertFile = os.Getenv("ORDERS_TLS_CERT_FILE")
	cfg.TLSKeyFile = os.Getenv("ORDERS_TLS_KEY_FILE")
	cfg.TLSClientCAFile = os.Getenv("ORDERS_TLS_CLIENT_CA_FILE")

	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return Config{}, ErrTLSPair
	}

	if cfg.TLSClientCAFile != "" && (cfg.TLSCertFile == "" || cfg.TLSKeyFile == "") {
		return Config{}, ErrClientCAWithoutTLS
	}

	return cfg, nil
}

// BuildTLSConfig создаёт *tls.Config на основе путей к сертификатам.
// Если TLS не настроен (TLSCertFile пустой), возвращает nil, nil.
func BuildTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.TLSCertFile == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("загрузка сертификата сервера: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.TLSClientCAFile != "" {
		caCert, err := os.ReadFile(cfg.TLSClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("чтение клиентского CA: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("не удалось разобрать client CA из %s", cfg.TLSClientCAFile)
		}

		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caPool
	}

	return tlsConfig, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("ORDERS_LOG_LEVEL=%q: ожидается debug|info|warn|error", raw)
	}
}
