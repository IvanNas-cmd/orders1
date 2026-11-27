package order_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	ordersv1 "github.com/gulmix/orders/internal/api/orders/v1"
	grpcController "github.com/gulmix/orders/internal/controllers/grpc/order"
	domain "github.com/gulmix/orders/internal/models/order"
	orderRepo "github.com/gulmix/orders/internal/repositories/order"
	orderService "github.com/gulmix/orders/internal/services/order"
)

const bufSize = 1024 * 1024

func setupGRPCTest(t *testing.T) (ordersv1.OrderServiceClient, *orderService.Service, func()) {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()

	repo := orderRepo.NewRepository()
	svc := orderService.NewService(repo)
	ctrl := grpcController.NewController(svc)

	ordersv1.RegisterOrderServiceServer(s, ctrl)

	go func() {
		_ = s.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}

	client := ordersv1.NewOrderServiceClient(conn)

	cleanup := func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}

	return client, svc, cleanup
}

// 1. Create → Get возвращает то же самое; Get несуществующего — NotFound.
func TestOrderService_CreateAndGet(t *testing.T) {
	client, _, cleanup := setupGRPCTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Создание заказа
	createReq := &ordersv1.CreateOrderRequest{
		CustomerId: "cust_42",
		Items: []*ordersv1.Item{
			{Sku: "book", Qty: 2, PriceCents: 1500},
			{Sku: "pen", Qty: 3, PriceCents: 200},
		},
	}

	createResp, err := client.CreateOrder(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	created := createResp.GetOrder()
	if created.GetId() == "" {
		t.Fatal("expected non-empty order ID")
	}
	if created.GetCustomerId() != "cust_42" {
		t.Errorf("got customerId %q, want 'cust_42'", created.GetCustomerId())
	}
	if created.GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_CREATED {
		t.Errorf("got status %v, want ORDER_STATUS_CREATED", created.GetStatus())
	}
	if created.GetTotalCents() != 3600 {
		t.Errorf("got total %d, want 3600", created.GetTotalCents())
	}

	// 2. Получение созданного заказа
	getResp, err := client.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetOrder failed: %v", err)
	}

	got := getResp.GetOrder()
	if got.GetId() != created.GetId() {
		t.Errorf("got id %q, want %q", got.GetId(), created.GetId())
	}
	if got.GetTotalCents() != created.GetTotalCents() {
		t.Errorf("got total %d, want %d", got.GetTotalCents(), created.GetTotalCents())
	}

	// 3. Получение несуществующего заказа -> NotFound
	_, err = client.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: "ord_missing_999"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound for missing order, got: %v", err)
	}

	// 4. Ошибки валидации входных данных -> InvalidArgument
	invalidCases := []struct {
		name string
		req  *ordersv1.CreateOrderRequest
	}{
		{
			name: "пустой customer_id",
			req: &ordersv1.CreateOrderRequest{
				CustomerId: "",
				Items:      []*ordersv1.Item{{Sku: "item", Qty: 1, PriceCents: 100}},
			},
		},
		{
			name: "нет позиций",
			req: &ordersv1.CreateOrderRequest{
				CustomerId: "cust_1",
				Items:      nil,
			},
		},
		{
			name: "qty <= 0",
			req: &ordersv1.CreateOrderRequest{
				CustomerId: "cust_1",
				Items:      []*ordersv1.Item{{Sku: "item", Qty: 0, PriceCents: 100}},
			},
		},
		{
			name: "price_cents < 0",
			req: &ordersv1.CreateOrderRequest{
				CustomerId: "cust_1",
				Items:      []*ordersv1.Item{{Sku: "item", Qty: 1, PriceCents: -50}},
			},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.CreateOrder(ctx, tc.req)
			if status.Code(err) != codes.InvalidArgument {
				t.Errorf("expected InvalidArgument, got code %v (err: %v)", status.Code(err), err)
			}
		})
	}
}

// 2. Недопустимый переход — FailedPrecondition.
func TestOrderService_InvalidTransition(t *testing.T) {
	client, _, cleanup := setupGRPCTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	createResp, err := client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{
		CustomerId: "cust_test",
		Items:      []*ordersv1.Item{{Sku: "item", Qty: 1, PriceCents: 100}},
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	orderID := createResp.GetOrder().GetId()

	// Попытка недопустимого перехода: created -> delivered (напрямую)
	_, err = client.UpdateOrderStatus(ctx, &ordersv1.UpdateOrderStatusRequest{
		Id:     orderID,
		Status: ordersv1.OrderStatus_ORDER_STATUS_DELIVERED,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition for created -> delivered, got code: %v (err: %v)", status.Code(err), err)
	}

	// Попытка передать UNSPECIFIED статус -> InvalidArgument
	_, err = client.UpdateOrderStatus(ctx, &ordersv1.UpdateOrderStatusRequest{
		Id:     orderID,
		Status: ordersv1.OrderStatus_ORDER_STATUS_UNSPECIFIED,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for UNSPECIFIED, got: %v", err)
	}

	// Допустимый переход: created -> paid
	updResp, err := client.UpdateOrderStatus(ctx, &ordersv1.UpdateOrderStatusRequest{
		Id:     orderID,
		Status: ordersv1.OrderStatus_ORDER_STATUS_PAID,
	})
	if err != nil {
		t.Fatalf("expected valid transition created -> paid, got: %v", err)
	}
	if updResp.GetOrder().GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_PAID {
		t.Errorf("got status %v, want ORDER_STATUS_PAID", updResp.GetOrder().GetStatus())
	}

	// Попытка отката назад: paid -> created
	_, err = client.UpdateOrderStatus(ctx, &ordersv1.UpdateOrderStatusRequest{
		Id:     orderID,
		Status: ordersv1.OrderStatus_ORDER_STATUS_CREATED,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition for paid -> created, got: %v", err)
	}
}

// 3. Поток. Подписались, сменили статус дважды до delivered.
// Пришло ровно три события в правильном порядке, потом io.EOF.
func TestOrderService_WatchOrder_Stream(t *testing.T) {
	client, _, cleanup := setupGRPCTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Создаём заказ (он сразу created)
	createResp, err := client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{
		CustomerId: "cust_stream",
		Items:      []*ordersv1.Item{{Sku: "item", Qty: 1, PriceCents: 500}},
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	orderID := createResp.GetOrder().GetId()

	// Переводим в paid перед открытием потока, чтобы дальше сделать ровно 2 смены статуса
	// paid -> shipped -> delivered
	_, err = client.UpdateOrderStatus(ctx, &ordersv1.UpdateOrderStatusRequest{
		Id:     orderID,
		Status: ordersv1.OrderStatus_ORDER_STATUS_PAID,
	})
	if err != nil {
		t.Fatalf("initial update to paid failed: %v", err)
	}

	// Подключаем поток WatchOrder
	stream, err := client.WatchOrder(ctx, &ordersv1.WatchOrderRequest{Id: orderID})
	if err != nil {
		t.Fatalf("WatchOrder failed: %v", err)
	}

	// Первое событие должно прийти сразу — текущий статус (ORDER_STATUS_PAID)
	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv first event failed: %v", err)
	}
	if first.GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_PAID {
		t.Fatalf("first event status = %v, want ORDER_STATUS_PAID", first.GetStatus())
	}

	// Меняем статус дважды до delivered
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = client.UpdateOrderStatus(context.Background(), &ordersv1.UpdateOrderStatusRequest{
			Id:     orderID,
			Status: ordersv1.OrderStatus_ORDER_STATUS_SHIPPED,
		})

		time.Sleep(20 * time.Millisecond)
		_, _ = client.UpdateOrderStatus(context.Background(), &ordersv1.UpdateOrderStatusRequest{
			Id:     orderID,
			Status: ordersv1.OrderStatus_ORDER_STATUS_DELIVERED,
		})
	}()

	// Второе событие: shipped
	second, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv second event failed: %v", err)
	}
	if second.GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_SHIPPED {
		t.Fatalf("second event status = %v, want ORDER_STATUS_SHIPPED", second.GetStatus())
	}

	// Третье событие: delivered (финальный)
	third, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv third event failed: %v", err)
	}
	if third.GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_DELIVERED {
		t.Fatalf("third event status = %v, want ORDER_STATUS_DELIVERED", third.GetStatus())
	}

	// Дальше поток должен закрыться сам (возврат io.EOF)
	fourth, err := stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after delivered, got event=%v err=%v", fourth, err)
	}
}

// 4. Отмена. Клиент отменил контекст — подписка на стороне сервера удалена.
// Доказываем это тестом через счётчик подписчиков в сервисе.
func TestOrderService_WatchOrder_Cancellation(t *testing.T) {
	client, svc, cleanup := setupGRPCTest(t)
	defer cleanup()

	// Создаём заказ
	createResp, err := client.CreateOrder(context.Background(), &ordersv1.CreateOrderRequest{
		CustomerId: "cust_cancel",
		Items:      []*ordersv1.Item{{Sku: "item", Qty: 1, PriceCents: 100}},
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	orderID := createResp.GetOrder().GetId()

	cancelCtx, cancel := context.WithCancel(context.Background())

	stream, err := client.WatchOrder(cancelCtx, &ordersv1.WatchOrderRequest{Id: orderID})
	if err != nil {
		t.Fatalf("WatchOrder failed: %v", err)
	}

	// Вычитываем первое сообщение (текущий статус)
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv initial message failed: %v", err)
	}
	if msg.GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_CREATED {
		t.Fatalf("got status %v, want ORDER_STATUS_CREATED", msg.GetStatus())
	}

	// Проверяем, что на стороне сервера зарегистрирован ровно 1 подписчик
	if count := svc.SubscriberCount(orderID); count != 1 {
		t.Fatalf("expected 1 active subscriber before cancellation, got %d", count)
	}

	// Отменяем контекст на клиенте
	cancel()

	// Ожидаем завершения стрима на клиенте
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error after context cancellation, got nil")
	}

	// Проверяем, что подписка удалена из памяти сервиса
	var finalCount int
	for i := 0; i < 50; i++ {
		finalCount = svc.SubscriberCount(orderID)
		if finalCount == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if finalCount != 0 {
		t.Errorf("expected 0 active subscribers after cancellation, got %d", finalCount)
	}
}

// 5. Заказ, созданный по HTTP, виден по gRPC и наоборот.
func TestCrossProtocol_HTTPAndGRPC(t *testing.T) {
	client, svc, cleanup := setupGRPCTest(t)
	defer cleanup()

	// 1. Создаём заказ через сервисный слой (аналог вызова HTTP-контроллера)
	httpCreated, err := svc.Create("cust_http", []domain.Item{
		{SKU: "item_http", Qty: 2, PriceCents: 1000},
	})
	if err != nil {
		t.Fatalf("svc.Create failed: %v", err)
	}

	// 2. Проверяем, что созданный заказ виден через gRPC
	grpcGot, err := client.GetOrder(context.Background(), &ordersv1.GetOrderRequest{Id: httpCreated.ID})
	if err != nil {
		t.Fatalf("client.GetOrder failed: %v", err)
	}
	if grpcGot.GetOrder().GetCustomerId() != "cust_http" {
		t.Errorf("got customerId %q, want 'cust_http'", grpcGot.GetOrder().GetCustomerId())
	}

	// 3. Обновляем статус через gRPC: created -> paid
	_, err = client.UpdateOrderStatus(context.Background(), &ordersv1.UpdateOrderStatusRequest{
		Id:     httpCreated.ID,
		Status: ordersv1.OrderStatus_ORDER_STATUS_PAID,
	})
	if err != nil {
		t.Fatalf("client.UpdateOrderStatus failed: %v", err)
	}

	// 4. Проверяем, что в сервисном слое статус изменился на "paid"
	httpGot, err := svc.GetByID(httpCreated.ID)
	if err != nil {
		t.Fatalf("svc.GetByID failed: %v", err)
	}
	if httpGot.Status != "paid" {
		t.Errorf("got status %q, want 'paid'", httpGot.Status)
	}
}
