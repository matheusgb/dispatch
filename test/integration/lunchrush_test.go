//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matheusgb/lunch-rush/internal/courier"
	"github.com/matheusgb/lunch-rush/internal/delivery"
	"github.com/matheusgb/lunch-rush/internal/lunchrush"
)

func setupOfferedDelivery(t *testing.T, ctx context.Context, key string) string {
	t.Helper()
	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: key})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE deliveries SET state = $1 WHERE id = $2`, delivery.ReadyForLunchRush, created.ID); err != nil {
		t.Fatalf("mover para ready_for_lunchrush: %v", err)
	}

	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})
	if err := svc.Offer(ctx, created.ID, time.Hour); err != nil {
		t.Fatalf("oferecer entrega: %v", err)
	}
	return created.ID
}

// TestLunchRush_TwentyConcurrentAssignsProduceExactlyOne é o gate principal do
// tier 1: vinte tentativas simultâneas de aceite para a mesma entrega e o
// mesmo entregador devem resultar em exatamente uma atribuição válida.
func TestLunchRush_TwentyConcurrentAssignsProduceExactlyOne(t *testing.T) {
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
	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})

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
		case errors.Is(err, lunchrush.ErrNotOffered), errors.Is(err, lunchrush.ErrCourierAlreadyActive):
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

// TestLunchRush_CourierCannotHoldTwoActiveDeliveries cobre a invariante 2 do
// outro ângulo: duas entregas distintas disputando o mesmo entregador
// disponível só deixam uma delas assigned.
func TestLunchRush_CourierCannotHoldTwoActiveDeliveries(t *testing.T) {
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
	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})

	errA := svc.Assign(ctx, deliveryA, c.ID)
	errB := svc.Assign(ctx, deliveryB, c.ID)

	successes := 0
	for _, err := range []error{errA, errB} {
		if err == nil {
			successes++
		} else if !errors.Is(err, lunchrush.ErrCourierAlreadyActive) {
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("entregador ficou com %d entregas ativas, want 1", successes)
	}
}

func TestLunchRush_DeclineRecyclesToReadyForLunchRush(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupOfferedDelivery(t, ctx, t.Name())

	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})
	if err := svc.Decline(ctx, deliveryID); err != nil {
		t.Fatalf("recusar oferta: %v", err)
	}

	got, err := delivery.NewRepository(pool).Get(ctx, deliveryID)
	if err != nil {
		t.Fatalf("buscar entrega: %v", err)
	}
	if got.State != delivery.ReadyForLunchRush {
		t.Fatalf("estado após recusa = %s, want %s", got.State, delivery.ReadyForLunchRush)
	}
}

func TestLunchRush_ExpireOverdueOffersRecyclesAfterDeadline(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE deliveries SET state = $1 WHERE id = $2`, delivery.ReadyForLunchRush, created.ID); err != nil {
		t.Fatalf("mover para ready_for_lunchrush: %v", err)
	}

	offerTime := time.Now()
	offerSvc := lunchrush.NewService(pool, lunchrush.FixedClock{At: offerTime})
	if err := offerSvc.Offer(ctx, created.ID, time.Minute); err != nil {
		t.Fatalf("oferecer entrega: %v", err)
	}

	// Ainda dentro do prazo: nada deveria expirar.
	stillValidSvc := lunchrush.NewService(pool, lunchrush.FixedClock{At: offerTime.Add(30 * time.Second)})
	if n, err := stillValidSvc.ExpireOverdueOffers(ctx); err != nil || n != 0 {
		t.Fatalf("expirou %d antes do prazo (err=%v), want 0", n, err)
	}

	// Depois do prazo: a oferta expira e volta para ready_for_lunchrush.
	afterDeadlineSvc := lunchrush.NewService(pool, lunchrush.FixedClock{At: offerTime.Add(2 * time.Minute)})
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
	if got.State != delivery.ReadyForLunchRush {
		t.Fatalf("estado após expiração = %s, want %s", got.State, delivery.ReadyForLunchRush)
	}
}

func TestLunchRush_CourierFreedAfterDeliveryCompletes(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	couriers := courier.NewRepository(pool)
	c, err := couriers.Register(ctx, "entregador-reciclado")
	if err != nil {
		t.Fatalf("cadastrar entregador: %v", err)
	}
	if err := couriers.SetAvailability(ctx, c.ID, true); err != nil {
		t.Fatalf("disponibilizar entregador: %v", err)
	}

	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})
	deliveries := delivery.NewRepository(pool)

	first := setupOfferedDelivery(t, ctx, t.Name()+"-first")
	if err := svc.Assign(ctx, first, c.ID); err != nil {
		t.Fatalf("atribuir primeira entrega: %v", err)
	}

	second := setupOfferedDelivery(t, ctx, t.Name()+"-second")
	if err := svc.Assign(ctx, second, c.ID); !errors.Is(err, lunchrush.ErrCourierAlreadyActive) {
		t.Fatalf("segunda atribuição deveria falhar com entregador ocupado, got %v", err)
	}

	if err := svc.PickUp(ctx, first); err != nil {
		t.Fatalf("coletar primeira entrega: %v", err)
	}
	if err := svc.Deliver(ctx, first); err != nil {
		t.Fatalf("concluir primeira entrega: %v", err)
	}

	if err := svc.Assign(ctx, second, c.ID); err != nil {
		t.Fatalf("entregador deveria estar livre para a segunda entrega: %v", err)
	}

	got, err := deliveries.Get(ctx, second)
	if err != nil {
		t.Fatalf("buscar segunda entrega: %v", err)
	}
	if got.State != delivery.Assigned {
		t.Fatalf("estado da segunda entrega = %s, want %s", got.State, delivery.Assigned)
	}
}
