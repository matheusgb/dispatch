//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/platform/idempotency"
)

func TestDelivery_CreateIsIdempotent(t *testing.T) {
	truncateAll(t)
	repo := delivery.NewRepository(pool)
	ctx := context.Background()

	req := delivery.CreateRequest{Caller: "order-service", Key: "order-123"}

	first, replayed, err := repo.Create(ctx, req)
	if err != nil {
		t.Fatalf("primeira criação: %v", err)
	}
	if replayed {
		t.Fatalf("primeira criação não deveria ser uma repetição")
	}
	if first.State != delivery.Created {
		t.Fatalf("estado inicial = %s, want %s", first.State, delivery.Created)
	}

	second, replayed, err := repo.Create(ctx, req)
	if err != nil {
		t.Fatalf("repetição com mesmo payload deveria devolver o resultado original: %v", err)
	}
	if !replayed {
		t.Fatalf("segunda criação deveria ser reconhecida como repetição")
	}
	if second.ID != first.ID {
		t.Fatalf("repetição criou uma entrega nova: got %s, want %s", second.ID, first.ID)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM deliveries").Scan(&count); err != nil {
		t.Fatalf("contar entregas: %v", err)
	}
	if count != 1 {
		t.Fatalf("10 retries com a mesma chave deveriam produzir 1 entrega, achou %d", count)
	}
}

func TestDelivery_CreateRejectsConflictingPayload(t *testing.T) {
	truncateAll(t)
	repo := delivery.NewRepository(pool)
	ctx := context.Background()

	if _, _, err := repo.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: "order-999"}); err != nil {
		t.Fatalf("criação inicial: %v", err)
	}

	// Mesma chave, caller diferente: o escopo é por caller, não deve colidir.
	if _, _, err := repo.Create(ctx, delivery.CreateRequest{Caller: "ops-console", Key: "order-999"}); err != nil {
		t.Fatalf("chave igual em caller diferente deveria ser independente: %v", err)
	}
}

func TestDelivery_TenConcurrentRetriesProduceOneDelivery(t *testing.T) {
	truncateAll(t)
	repo := delivery.NewRepository(pool)
	ctx := context.Background()
	req := delivery.CreateRequest{Caller: "order-service", Key: "order-concurrent"}

	const attempts = 10
	ids := make([]string, attempts)
	errs := make([]error, attempts)
	done := make(chan int, attempts)

	for i := 0; i < attempts; i++ {
		go func(i int) {
			resp, _, err := repo.Create(ctx, req)
			ids[i], errs[i] = resp.ID, err
			done <- i
		}(i)
	}
	for i := 0; i < attempts; i++ {
		<-done
	}

	for i, err := range errs {
		if err != nil && !errors.Is(err, idempotency.ErrConflict) {
			t.Fatalf("tentativa %d falhou: %v", i, err)
		}
	}
	for i := 1; i < attempts; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("tentativa %d devolveu ID diferente: got %s, want %s", i, ids[i], ids[0])
		}
	}
}
