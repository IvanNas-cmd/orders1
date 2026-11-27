package order

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	ordersv1 "github.com/gulmix/orders/internal/api/orders/v1"
	domain "github.com/gulmix/orders/internal/models/order"
)

// OrderService — интерфейс сервисного слоя для gRPC-контроллера.
type OrderService interface {
	Create(customerID string, items []domain.Item) (domain.Order, error)
	GetByID(id string) (domain.Order, error)
	UpdateStatus(id, newStatus string) (domain.Order, error)
	Subscribe(orderID string) (<-chan domain.Order, func())
}

// Controller реализует ordersv1.OrderServiceServer.
type Controller struct {
	ordersv1.UnimplementedOrderServiceServer
	service OrderService
}

// NewController создаёт gRPC-контроллер заказов.
func NewController(s OrderService) *Controller {
	return &Controller{service: s}
}

func (c *Controller) CreateOrder(ctx context.Context, req *ordersv1.CreateOrderRequest) (*ordersv1.CreateOrderResponse, error) {
	if req.GetCustomerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id cannot be empty")
	}
	if len(req.GetItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "order must contain at least one item")
	}

	items := make([]domain.Item, len(req.GetItems()))
	for i, item := range req.GetItems() {
		if item.GetQty() <= 0 {
			return nil, status.Error(codes.InvalidArgument, "item quantity must be greater than zero")
		}
		if item.GetPriceCents() < 0 {
			return nil, status.Error(codes.InvalidArgument, "price cannot be negative")
		}
		items[i] = domain.Item{
			SKU:        item.GetSku(),
			Qty:        int(item.GetQty()),
			PriceCents: item.GetPriceCents(),
		}
	}

	ord, err := c.service.Create(req.GetCustomerId(), items)
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &ordersv1.CreateOrderResponse{
		Order: toProtoOrder(ord),
	}, nil
}

func (c *Controller) GetOrder(ctx context.Context, req *ordersv1.GetOrderRequest) (*ordersv1.GetOrderResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id cannot be empty")
	}

	ord, err := c.service.GetByID(req.GetId())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &ordersv1.GetOrderResponse{
		Order: toProtoOrder(ord),
	}, nil
}

func (c *Controller) UpdateOrderStatus(ctx context.Context, req *ordersv1.UpdateOrderStatusRequest) (*ordersv1.UpdateOrderStatusResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id cannot be empty")
	}

	domainStatus, err := fromProtoStatus(req.GetStatus())
	if err != nil {
		return nil, err
	}

	ord, err := c.service.UpdateStatus(req.GetId(), domainStatus)
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &ordersv1.UpdateOrderStatusResponse{
		Order: toProtoOrder(ord),
	}, nil
}

func (c *Controller) WatchOrder(req *ordersv1.WatchOrderRequest, stream ordersv1.OrderService_WatchOrderServer) error {
	if req.GetId() == "" {
		return status.Error(codes.InvalidArgument, "id cannot be empty")
	}

	// 1. Получаем текущее состояние заказа
	current, err := c.service.GetByID(req.GetId())
	if err != nil {
		return toGRPCError(err)
	}

	// 2. Первым сообщением отдаём текущий статус
	if err := stream.Send(&ordersv1.WatchOrderResponse{
		OrderId:   current.ID,
		Status:    toProtoStatus(current.Status),
		ChangedAt: timestamppb.New(current.CreatedAt),
	}); err != nil {
		return err
	}

	// Если заказ уже в финальном статусе — поток завершается
	if isFinalStatus(current.Status) {
		return nil
	}

	// 3. Подписываемся на последующие изменения статуса
	eventCh, unsubscribe := c.service.Subscribe(req.GetId())
	defer unsubscribe()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ord, ok := <-eventCh:
			if !ok {
				return nil
			}

			if err := stream.Send(&ordersv1.WatchOrderResponse{
				OrderId:   ord.ID,
				Status:    toProtoStatus(ord.Status),
				ChangedAt: timestamppb.New(time.Now().UTC()),
			}); err != nil {
				return err
			}

			// Поток закрывается сам после финального статуса
			if isFinalStatus(ord.Status) {
				return nil
			}
		}
	}
}

func isFinalStatus(s string) bool {
	return s == domain.StatusDelivered || s == domain.StatusCancelled
}

func toProtoOrder(o domain.Order) *ordersv1.Order {
	items := make([]*ordersv1.Item, len(o.Items))
	for i, item := range o.Items {
		items[i] = &ordersv1.Item{
			Sku:        item.SKU,
			Qty:        int32(item.Qty),
			PriceCents: item.PriceCents,
		}
	}

	return &ordersv1.Order{
		Id:         o.ID,
		CustomerId: o.CustomerID,
		Status:     toProtoStatus(o.Status),
		Items:      items,
		TotalCents: o.TotalCents,
		CreatedAt:  timestamppb.New(o.CreatedAt),
	}
}

func toProtoStatus(s string) ordersv1.OrderStatus {
	switch s {
	case domain.StatusCreated:
		return ordersv1.OrderStatus_ORDER_STATUS_CREATED
	case domain.StatusPaid:
		return ordersv1.OrderStatus_ORDER_STATUS_PAID
	case domain.StatusShipped:
		return ordersv1.OrderStatus_ORDER_STATUS_SHIPPED
	case domain.StatusDelivered:
		return ordersv1.OrderStatus_ORDER_STATUS_DELIVERED
	case domain.StatusCancelled:
		return ordersv1.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return ordersv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func fromProtoStatus(s ordersv1.OrderStatus) (string, error) {
	switch s {
	case ordersv1.OrderStatus_ORDER_STATUS_CREATED:
		return domain.StatusCreated, nil
	case ordersv1.OrderStatus_ORDER_STATUS_PAID:
		return domain.StatusPaid, nil
	case ordersv1.OrderStatus_ORDER_STATUS_SHIPPED:
		return domain.StatusShipped, nil
	case ordersv1.OrderStatus_ORDER_STATUS_DELIVERED:
		return domain.StatusDelivered, nil
	case ordersv1.OrderStatus_ORDER_STATUS_CANCELLED:
		return domain.StatusCancelled, nil
	case ordersv1.OrderStatus_ORDER_STATUS_UNSPECIFIED:
		fallthrough
	default:
		return "", status.Error(codes.InvalidArgument, "invalid or unspecified order status")
	}
}

func toGRPCError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "order not found")
	case errors.Is(err, domain.ErrInvalidStatusTransition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrEmptyCustomer),
		errors.Is(err, domain.ErrNoItems),
		errors.Is(err, domain.ErrInvalidQuantity),
		errors.Is(err, domain.ErrInvalidPrice):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
