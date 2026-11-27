package grpc_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ordersv1 "github.com/gulmix/orders/internal/api/orders/v1"
	grpcController "github.com/gulmix/orders/internal/controllers/grpc/order"
	orderRepo "github.com/gulmix/orders/internal/repositories/order"
	grpcServer "github.com/gulmix/orders/internal/servers/grpc"
	orderService "github.com/gulmix/orders/internal/services/order"
)

func TestServer_StartAndGracefulStop(t *testing.T) {
	repo := orderRepo.NewRepository()
	svc := orderService.NewService(repo)
	ctrl := grpcController.NewController(svc)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := grpcServer.NewServer("127.0.0.1:0", ctrl, logger, 2*time.Second, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve()
	}()

	conn, err := grpc.NewClient(srv.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := ordersv1.NewOrderServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{
		CustomerId: "cust_srv_test",
		Items:      []*ordersv1.Item{{Sku: "item", Qty: 1, PriceCents: 100}},
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	// Остановка сервера
	srv.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("unexpected serve error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for server to stop")
	}
}
