//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/matheusgb/dispatch/internal/delivery"
	"github.com/matheusgb/dispatch/internal/tracking"
)

func setupDeliveryForTracking(t *testing.T, ctx context.Context) string {
	t.Helper()
	created, _, err := delivery.NewRepository(pool).Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}
	return created.ID
}

func TestTracking_LatePositionNeverOverridesNewer(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupDeliveryForTracking(t, ctx)
	repo := tracking.NewRepository(pool)

	newer := tracking.Position{DeliveryID: deliveryID, Epoch: 1, Sequence: 10, Latitude: -23.5, Longitude: -46.6, RecordedAt: time.Now()}
	current, err := repo.RecordPosition(ctx, newer)
	if err != nil || !current {
		t.Fatalf("posição mais nova deveria se tornar atual: current=%v err=%v", current, err)
	}

	older := tracking.Position{DeliveryID: deliveryID, Epoch: 1, Sequence: 5, Latitude: -23.6, Longitude: -46.7, RecordedAt: time.Now()}
	current, err = repo.RecordPosition(ctx, older)
	if err != nil {
		t.Fatalf("registrar posição atrasada: %v", err)
	}
	if current {
		t.Fatalf("posição atrasada não deveria se tornar atual")
	}

	got, err := repo.CurrentPosition(ctx, deliveryID)
	if err != nil {
		t.Fatalf("buscar posição atual: %v", err)
	}
	if got.Sequence != 10 {
		t.Fatalf("posição atual regrediu: sequence = %d, want 10", got.Sequence)
	}

	history, err := repo.History(ctx, deliveryID, 10)
	if err != nil {
		t.Fatalf("buscar histórico: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("histórico deveria ter as duas posições, tem %d", len(history))
	}
}

func TestTracking_NewEpochSupersedesOldSequence(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupDeliveryForTracking(t, ctx)
	repo := tracking.NewRepository(pool)

	if _, err := repo.RecordPosition(ctx, tracking.Position{DeliveryID: deliveryID, Epoch: 1, Sequence: 100, Latitude: 1, Longitude: 1, RecordedAt: time.Now()}); err != nil {
		t.Fatalf("posição do epoch 1: %v", err)
	}

	// Um novo epoch (reinício de sessão de tracking) com sequência baixa
	// ainda precisa superar o epoch anterior, mesmo com sequence menor.
	current, err := repo.RecordPosition(ctx, tracking.Position{DeliveryID: deliveryID, Epoch: 2, Sequence: 1, Latitude: 2, Longitude: 2, RecordedAt: time.Now()})
	if err != nil || !current {
		t.Fatalf("novo epoch deveria substituir o anterior: current=%v err=%v", current, err)
	}
}

func TestTracking_DuplicatePositionIsIdempotent(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupDeliveryForTracking(t, ctx)
	repo := tracking.NewRepository(pool)

	p := tracking.Position{DeliveryID: deliveryID, Epoch: 1, Sequence: 1, Latitude: 1, Longitude: 1, RecordedAt: time.Now()}
	if _, err := repo.RecordPosition(ctx, p); err != nil {
		t.Fatalf("primeira gravação: %v", err)
	}
	if _, err := repo.RecordPosition(ctx, p); err != nil {
		t.Fatalf("reenvio do mesmo ponto: %v", err)
	}

	history, err := repo.History(ctx, deliveryID, 10)
	if err != nil {
		t.Fatalf("buscar histórico: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("reenvio duplicado não deveria criar uma segunda linha no log, tem %d", len(history))
	}
}
