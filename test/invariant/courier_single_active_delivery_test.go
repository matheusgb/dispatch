//go:build integration

package invariant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matheusgb/lunch-rush/internal/courier"
	"github.com/matheusgb/lunch-rush/internal/delivery"
	"github.com/matheusgb/lunch-rush/internal/lunchrush"
)

// Invariante 2 do README: "um entregador possui no máximo uma entrega
// ativa". test/integration/lunchrush_test.go já cobre isso via
// TestLunchRush_CourierCannotHoldTwoActiveDeliveries; este teste é o mesmo
// invariante, rotulado como tal, exercitado com duas entregas distintas em
// vez de reenviar a mesma (a mesma propriedade, ângulo levemente diferente:
// aqui as duas entregas concorrem por atenção do MESMO entregador ao mesmo
// tempo, não em sequência).
func TestInvariant_CourierNeverHoldsTwoActiveDeliveriesConcurrently(t *testing.T) {
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

	deliveries := delivery.NewRepository(pool)
	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})

	ids := make([]string, 2)
	for i := range ids {
		created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name() + string(rune('A'+i))})
		if err != nil {
			t.Fatalf("criar entrega %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE deliveries SET state = $1 WHERE id = $2`, delivery.ReadyForLunchRush, created.ID); err != nil {
			t.Fatalf("mover entrega %d para ready_for_lunchrush: %v", i, err)
		}
		if err := svc.Offer(ctx, created.ID, time.Hour); err != nil {
			t.Fatalf("oferecer entrega %d: %v", i, err)
		}
		ids[i] = created.ID
	}

	errs := make([]error, 2)
	done := make(chan int, 2)
	for i, id := range ids {
		go func(i int, id string) {
			errs[i] = svc.Assign(ctx, id, c.ID)
			done <- i
		}(i, id)
	}
	for range ids {
		<-done
	}

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, lunchrush.ErrCourierAlreadyActive) {
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("invariante violado: courier ficou com %d entregas ativas simultâneas, want 1", successes)
	}
}
