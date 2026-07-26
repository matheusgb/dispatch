//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matheusgb/dispatch/internal/courier"
	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/dispatch"
)

func setupOfferedDelivery(t *testing.T, ctx context.Context, key string) string {
	t.Helper()
	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: key})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE deliveries SET state = $1 WHERE id = $2`, delivery.ReadyForDispatch, created.ID); err != nil {
		t.Fatalf("mover para ready_for_dispatch: %v", err)
	}

	svc := dispatch.NewService(pool, dispatch.FixedClock{At: time.Now()})
	if err := svc.Offer(ctx, created.ID, time.Hour); err != nil {
		t.Fatalf("oferecer entrega: %v", err)
	}
	return created.ID
}

// TestDispatch_TwentyConcurrentAssignsProduceExactlyOne é o gate principal do
// tier 1: vinte tentativas simultâneas de aceite para a mesma entrega e o
// mesmo entregador devem resultar em exatamente uma atribuição válida.
func TestDispatch_TwentyConcurrentAssignsProduceExactlyOne(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	couriers := courier.NewRepository(pool)
	c, err := couriers.Register(ctx, "entregador-disputado")
	if err != nil {
		t.Fatalf("cadastrar entregador: %v", err)
	}
	if err := couriers.SetAvailability(ctx, c.ID, true); err != nil {
		t.Fatalf("disponibilizar entregador: %v", err)
	}

	deliveryID := setupOfferedDelivery(t, ctx, t.Name())
	svc := dispatch.NewService(pool, dispatch.FixedClock{At: time.Now()})

	const attempts = 20
	errs := make([]error, attempts)
	done := make(chan int, attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			errs[i] = svc.Assign(ctx, deliveryID, c.ID)
			done <- i
		}(i)
	}
	for i := 0; i < attempts; i++ {
		<-done
	}

	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, dispatch.ErrNotOffered), errors.Is(err, dispatch.ErrCourierAlreadyActive):
			// esperado: a entrega já saiu de offered, ou o entregador já
			// estava ativo quando esta tentativa tentou o commit.
		default:
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("20 disputas concorrentes produziram %d atribuições, want exatamente 1", successes)
	}

	got, err := delivery.NewRepository(pool).Get(ctx, deliveryID)
	if err != nil {
		t.Fatalf("buscar entrega: %v", err)
	}
	if got.State != delivery.Assigned {
		t.Fatalf("estado final = %s, want %s", got.State, delivery.Assigned)
	}
	if got.CourierID == nil || *got.CourierID != c.ID {
		t.Fatalf("entregador atribuído incorreto: %v", got.CourierID)
	}
}

// TestDispatch_CourierCannotHoldTwoActiveDeliveries cobre a invariante 2 do
// outro ângulo: duas entregas distintas disputando o mesmo entregador
// disponível só deixam uma delas assigned.
func TestDispatch_CourierCannotHoldTwoActiveDeliveries(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	couriers := courier.NewRepository(pool)
	c, err := couriers.Register(ctx, "entregador-unico")
	if err != nil {
		t.Fatalf("cadastrar entregador: %v", err)
	}
	if err := couriers.SetAvailability(ctx, c.ID, true); err != nil {
		t.Fatalf("disponibilizar entregador: %v", err)
	}

	deliveryA := setupOfferedDelivery(t, ctx, t.Name()+"-a")
	deliveryB := setupOfferedDelivery(t, ctx, t.Name()+"-b")
	svc := dispatch.NewService(pool, dispatch.FixedClock{At: time.Now()})

	errA := svc.Assign(ctx, deliveryA, c.ID)
	errB := svc.Assign(ctx, deliveryB, c.ID)

	successes := 0
	for _, err := range []error{errA, errB} {
		if err == nil {
			successes++
		} else if !errors.Is(err, dispatch.ErrCourierAlreadyActive) {
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("entregador ficou com %d entregas ativas, want 1", successes)
	}
}

func TestDispatch_DeclineRecyclesToReadyForDispatch(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupOfferedDelivery(t, ctx, t.Name())

	svc := dispatch.NewService(pool, dispatch.FixedClock{At: time.Now()})
	if err := svc.Decline(ctx, deliveryID); err != nil {
		t.Fatalf("recusar oferta: %v", err)
	}

	got, err := delivery.NewRepository(pool).Get(ctx, deliveryID)
	if err != nil {
		t.Fatalf("buscar entrega: %v", err)
	}
	if got.State != delivery.ReadyForDispatch {
		t.Fatalf("estado após recusa = %s, want %s", got.State, delivery.ReadyForDispatch)
	}
}

func TestDispatch_ExpireOverdueOffersRecyclesAfterDeadline(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE deliveries SET state = $1 WHERE id = $2`, delivery.ReadyForDispatch, created.ID); err != nil {
		t.Fatalf("mover para ready_for_dispatch: %v", err)
	}

	offerTime := time.Now()
	offerSvc := dispatch.NewService(pool, dispatch.FixedClock{At: offerTime})
	if err := offerSvc.Offer(ctx, created.ID, time.Minute); err != nil {
		t.Fatalf("oferecer entrega: %v", err)
	}

	// Ainda dentro do prazo: nada deveria expirar.
	stillValidSvc := dispatch.NewService(pool, dispatch.FixedClock{At: offerTime.Add(30 * time.Second)})
	if n, err := stillValidSvc.ExpireOverdueOffers(ctx); err != nil || n != 0 {
		t.Fatalf("expirou %d antes do prazo (err=%v), want 0", n, err)
	}

	// Depois do prazo: a oferta expira e volta para ready_for_dispatch.
	afterDeadlineSvc := dispatch.NewService(pool, dispatch.FixedClock{At: offerTime.Add(2 * time.Minute)})
	n, err := afterDeadlineSvc.ExpireOverdueOffers(ctx)
	if err != nil {
		t.Fatalf("expirar ofertas vencidas: %v", err)
	}
	if n != 1 {
		t.Fatalf("expirou %d ofertas, want 1", n)
	}

	got, err := deliveries.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("buscar entrega: %v", err)
	}
	if got.State != delivery.ReadyForDispatch {
		t.Fatalf("estado após expiração = %s, want %s", got.State, delivery.ReadyForDispatch)
	}
}
