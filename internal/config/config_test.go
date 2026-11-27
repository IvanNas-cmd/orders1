package config_test

import (
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	. "github.com/gulmix/orders/internal/config"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		want        Config
		wantErr     bool
		expectErrIs error
	}{
		{
			name: "значения по умолчанию",
			env:  nil,
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelInfo,
			},
		},
		{
			name: "переопределение из окружения",
			env: map[string]string{
				"ORDERS_HTTP_ADDR":        ":8081",
				"ORDERS_GRPC_ADDR":        ":9091",
				"ORDERS_SHUTDOWN_TIMEOUT": "3s",
				"ORDERS_LOG_LEVEL":        "debug",
			},
			want: Config{
				HTTPAddr:        ":8081",
				GRPCAddr:        ":9091",
				ShutdownTimeout: 3 * time.Second,
				LogLevel:        slog.LevelDebug,
			},
		},
		{
			name: "уровень info задан явно",
			env:  map[string]string{"ORDERS_LOG_LEVEL": "info"},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelInfo,
			},
		},
		{
			name: "уровень warn",
			env:  map[string]string{"ORDERS_LOG_LEVEL": "warn"},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelWarn,
			},
		},
		{
			name: "warning — синоним warn",
			env:  map[string]string{"ORDERS_LOG_LEVEL": "warning"},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelWarn,
			},
		},
		{
			name: "регистр и пробелы не важны",
			env:  map[string]string{"ORDERS_LOG_LEVEL": "  ERROR "},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelError,
			},
		},
		{
			name: "пустой адрес — значение по умолчанию",
			env:  map[string]string{"ORDERS_HTTP_ADDR": "", "ORDERS_GRPC_ADDR": ""},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelInfo,
			},
		},
		{
			name:    "некорректный таймаут — ошибка на старте",
			env:     map[string]string{"ORDERS_SHUTDOWN_TIMEOUT": "быстро"},
			wantErr: true,
		},
		{
			name:    "неизвестный уровень логирования — ошибка на старте",
			env:     map[string]string{"ORDERS_LOG_LEVEL": "trace"},
			wantErr: true,
		},
		{
			name: "TLS настроен корректно (заданы обе переменные)",
			env: map[string]string{
				"ORDERS_TLS_CERT_FILE": "/path/to/cert.pem",
				"ORDERS_TLS_KEY_FILE":  "/path/to/key.pem",
			},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelInfo,
				TLSCertFile:     "/path/to/cert.pem",
				TLSKeyFile:      "/path/to/key.pem",
			},
		},
		{
			name: "задан только ORDERS_TLS_CERT_FILE — ошибка на старте",
			env: map[string]string{
				"ORDERS_TLS_CERT_FILE": "/path/to/cert.pem",
			},
			wantErr:     true,
			expectErrIs: ErrTLSPair,
		},
		{
			name: "задан только ORDERS_TLS_KEY_FILE — ошибка на старте",
			env: map[string]string{
				"ORDERS_TLS_KEY_FILE": "/path/to/key.pem",
			},
			wantErr:     true,
			expectErrIs: ErrTLSPair,
		},
		{
			name: "mTLS настроен корректно (заданы сертификат, ключ и client CA)",
			env: map[string]string{
				"ORDERS_TLS_CERT_FILE":      "/path/to/cert.pem",
				"ORDERS_TLS_KEY_FILE":       "/path/to/key.pem",
				"ORDERS_TLS_CLIENT_CA_FILE": "/path/to/ca.pem",
			},
			want: Config{
				HTTPAddr:        ":8080",
				GRPCAddr:        ":9090",
				ShutdownTimeout: 10 * time.Second,
				LogLevel:        slog.LevelInfo,
				TLSCertFile:     "/path/to/cert.pem",
				TLSKeyFile:      "/path/to/key.pem",
				TLSClientCAFile: "/path/to/ca.pem",
			},
		},
		{
			name: "задан ORDERS_TLS_CLIENT_CA_FILE без сертификатов сервера — ошибка на старте",
			env: map[string]string{
				"ORDERS_TLS_CLIENT_CA_FILE": "/path/to/ca.pem",
			},
			wantErr:     true,
			expectErrIs: ErrClientCAWithoutTLS,
		},
		{
			name: "задан ORDERS_TLS_CLIENT_CA_FILE и только ключ — ошибка на старте",
			env: map[string]string{
				"ORDERS_TLS_CLIENT_CA_FILE": "/path/to/ca.pem",
				"ORDERS_TLS_KEY_FILE":       "/path/to/key.pem",
			},
			wantErr:     true,
			expectErrIs: ErrTLSPair,
		},
		{
			name: "задан ORDERS_TLS_CLIENT_CA_FILE и только сертификат — ошибка на старте",
			env: map[string]string{
				"ORDERS_TLS_CLIENT_CA_FILE": "/path/to/ca.pem",
				"ORDERS_TLS_CERT_FILE":      "/path/to/cert.pem",
			},
			wantErr:     true,
			expectErrIs: ErrTLSPair,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			got, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() = %+v, ожидалась ошибка", got)
				}
				if tt.expectErrIs != nil && !errors.Is(err, tt.expectErrIs) {
					t.Fatalf("Load() error = %v, want errors.Is(..., %v)", err, tt.expectErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() вернул неожиданную ошибку: %v", err)
			}
			if got != tt.want {
				t.Errorf("Load() = %+v, ожидалось %+v", got, tt.want)
			}
		})
	}
}

func TestBuildTLSConfig_Disabled(t *testing.T) {
	cfg := Config{}
	tlsCfg, err := BuildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsCfg != nil {
		t.Error("expected nil *tls.Config when TLSCertFile is empty")
	}
}

func TestBuildTLSConfig_InvalidFiles(t *testing.T) {
	cfg := Config{
		TLSCertFile: "/non/existent/cert.pem",
		TLSKeyFile:  "/non/existent/key.pem",
	}
	_, err := BuildTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error on non-existent cert/key files, got nil")
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for _, key := range []string{
		"ORDERS_HTTP_ADDR",
		"ORDERS_GRPC_ADDR",
		"ORDERS_SHUTDOWN_TIMEOUT",
		"ORDERS_LOG_LEVEL",
		"ORDERS_TLS_CERT_FILE",
		"ORDERS_TLS_KEY_FILE",
		"ORDERS_TLS_CLIENT_CA_FILE",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("не удалось снять %s: %v", key, err)
		}
	}

	for k, v := range env {
		t.Setenv(k, v)
	}
}
