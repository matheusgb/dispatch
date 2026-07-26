//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matheusgb/dispatch/internal/courier"
	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/dispatch"
)

// BenchmarkDelivery_Create mede o caminho mais quente do tier 1: uma
// criação de entrega, com o ledger de idempotência, ponta a ponta contra o
// PostgreSQL local.
func BenchmarkDelivery_Create(b *testing.B) {
	truncateAllBench(b)
	repo := delivery.NewRepository(pool)
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-create-%d", i)
		if _, _, err := repo.Create(ctx, delivery.CreateRequest{Caller: "bench", Key: key}); err != nil {
			b.Fatalf("create: %v", err)
		}
	}
}

// BenchmarkDispatch_Assign mede o aceite concorrente: cada iteração já parte
// de uma entrega em offered e um entregador disponível, para isolar o custo
// do UPDATE condicional do custo de preparar o cenário.
func BenchmarkDispatch_Assign(b *testing.B) {
	truncateAllBench(b)
	ctx := context.Background()
	deliveries := delivery.NewRepository(pool)
	couriers := courier.NewRepository(pool)
	svc := dispatch.NewService(pool, dispatch.FixedClock{At: time.Now()})

	type pair struct{ deliveryID, courierID string }
	pairs := make([]pair, b.N)
	for i := 0; i < b.N; i++ {
		created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "bench", Key: uuid.NewString()})
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE deliveries SET state = $1 WHERE id = $2`, delivery.ReadyForDispatch, created.ID); err != nil {
			b.Fatalf("mover para ready_for_dispatch: %v", err)
		}
		if err := svc.Offer(ctx, created.ID, time.Hour); err != nil {
			b.Fatalf("oferecer: %v", err)
		}
		c, err := couriers.Register(ctx, "bench-courier")
		if err != nil {
			b.Fatalf("cadastrar entregador: %v", err)
		}
		if err := couriers.SetAvailability(ctx, c.ID, true); err != nil {
			b.Fatalf("disponibilizar: %v", err)
		}
		pairs[i] = pair{deliveryID: created.ID, courierID: c.ID}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := svc.Assign(ctx, pairs[i].deliveryID, pairs[i].courierID); err != nil {
			b.Fatalf("assign: %v", err)
		}
	}
}

func truncateAllBench(b *testing.B) {
	b.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE idempotency_keys, delivery_transitions, deliveries, couriers
	`)
	if err != nil {
		b.Fatalf("truncate: %v", err)
	}
}
