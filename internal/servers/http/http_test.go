package http_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	httpServer "github.com/gulmix/orders/internal/servers/http"
)

func TestServer_StartAndShutdown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httpServer.NewServer("127.0.0.1:0", mux, logger, 2*time.Second, nil)

	go func() {
		_ = srv.Serve()
	}()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}
