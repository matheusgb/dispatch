//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matheusgb/lunch-rush/internal/fencing"
)

// TestFencing_StaleEpochWriterNeverWrites é o gate multi-shard/multi-epoch
// do tier 5, equivalente a TestLunchRush_TwentyConcurrentAssignsProduceExactlyOne
// do tier 1: um writer com epoch antigo (representando uma região que já
// perdeu a lease, ou um processo que ainda não descobriu o failover) nunca
// consegue criar um assignment, mesmo disparando muitas tentativas
// concorrentes contra o mesmo shard onde um writer novo (epoch atual)
// também está criando assignments. Prova em código real a mesma
// propriedade que docs/tla/LunchRushFencing.tla verifica formalmente
// (Safety / NoStaleAssignEverHappened).
func TestFencing_StaleEpochWriterNeverWrites(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	svc := fencing.NewService(pool, fencing.SystemClock{})

	shardID := "shard-test-1"

	// writer "region-a" promove primeiro: epoch 1.
	staleFence, err := svc.Promote(ctx, shardID, "region-a", time.Hour)
	if err != nil {
		t.Fatalf("promote inicial: %v", err)
	}
	if staleFence.Epoch != 1 {
		t.Fatalf("epoch inicial esperado 1, veio %d", staleFence.Epoch)
	}

	// A lease de "region-a" expira (simulado por lease_until no passado) e
	// "region-b" assume: epoch 2. "region-a" continua achando que o epoch
	// vigente é 1 (staleFence), como um processo que não soube do failover.
	if _, err := pool.Exec(ctx, `UPDATE lunchrush_fences SET lease_until = now() - interval '1 second' WHERE shard_id = $1`, shardID); err != nil {
		t.Fatalf("expirar lease: %v", err)
	}
	currentFence, err := svc.Promote(ctx, shardID, "region-b", time.Hour)
	if err != nil {
		t.Fatalf("promote de failover: %v", err)
	}
	if currentFence.Epoch != 2 {
		t.Fatalf("epoch pós-failover esperado 2, veio %d", currentFence.Epoch)
	}

	const attempts = 20
	deliveryIDs := make([]string, attempts)
	courierIDs := make([]string, attempts)
	for i := 0; i < attempts; i++ {
		deliveryIDs[i] = uuid.NewString()
		courierIDs[i] = uuid.NewString()
		if _, err := pool.Exec(ctx, `INSERT INTO deliveries (id, state) VALUES ($1, 'offered')`, deliveryIDs[i]); err != nil {
			t.Fatalf("seed delivery %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO couriers (id, name, available) VALUES ($1, $2, true)`, courierIDs[i], "courier-"+courierIDs[i]); err != nil {
			t.Fatalf("seed courier %d: %v", i, err)
		}
	}

	var staleSuccesses, staleRejections, currentSuccesses int64
	var wg sync.WaitGroup
	wg.Add(attempts * 2)

	for i := 0; i < attempts; i++ {
		i := i
		// Metade das tentativas usa o writer com epoch velho (region-a,
		// epoch 1): todas devem ser rejeitadas com ErrStaleFence, sem
		// exceção, mesmo correndo ao mesmo tempo que o writer atual.
		go func() {
			defer wg.Done()
			_, err := svc.CreateAssignment(ctx, shardID, "region-a", staleFence.Epoch, deliveryIDs[i], courierIDs[i], 0)
			if err == nil {
				atomic.AddInt64(&staleSuccesses, 1)
			} else if errors.Is(err, fencing.ErrStaleFence) {
				atomic.AddInt64(&staleRejections, 1)
			} else {
				t.Errorf("writer velho: erro inesperado: %v", err)
			}
		}()
		// A outra metade usa deliveries/couriers diferentes com o writer
		// atual (region-b, epoch 2): todas devem ter sucesso.
		go func() {
			defer wg.Done()
			otherIdx := attempts + i // usado só para gerar IDs novos abaixo
			_ = otherIdx
			deliveryID := uuid.NewString()
			courierID := uuid.NewString()
			if _, err := pool.Exec(ctx, `INSERT INTO deliveries (id, state) VALUES ($1, 'offered')`, deliveryID); err != nil {
				t.Errorf("seed delivery writer atual: %v", err)
				return
			}
			if _, err := pool.Exec(ctx, `INSERT INTO couriers (id, name, available) VALUES ($1, $2, true)`, courierID, "courier-"+courierID); err != nil {
				t.Errorf("seed courier writer atual: %v", err)
				return
			}
			_, err := svc.CreateAssignment(ctx, shardID, "region-b", currentFence.Epoch, deliveryID, courierID, 0)
			if err != nil {
				t.Errorf("writer atual: erro inesperado: %v", err)
				return
			}
			atomic.AddInt64(&currentSuccesses, 1)
		}()
	}

	wg.Wait()

	if staleSuccesses != 0 {
		t.Fatalf("writer com epoch velho conseguiu escrever %d vez(es), esperado 0", staleSuccesses)
	}
	if staleRejections != attempts {
		t.Fatalf("esperava %d rejeições do writer velho, teve %d", attempts, staleRejections)
	}
	if currentSuccesses != attempts {
		t.Fatalf("esperava %d sucessos do writer atual, teve %d", attempts, currentSuccesses)
	}

	// Confirma no banco: nenhum assignment foi criado pelo epoch velho.
	var countStaleEpoch int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_assignments WHERE epoch = $1`, staleFence.Epoch).Scan(&countStaleEpoch); err != nil {
		t.Fatalf("contar assignments do epoch velho: %v", err)
	}
	if countStaleEpoch != 0 {
		t.Fatalf("encontrados %d assignments com o epoch velho no banco, esperado 0", countStaleEpoch)
	}

	var countCurrentEpoch int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_assignments WHERE epoch = $1`, currentFence.Epoch).Scan(&countCurrentEpoch); err != nil {
		t.Fatalf("contar assignments do epoch atual: %v", err)
	}
	if countCurrentEpoch != attempts {
		t.Fatalf("esperava %d assignments com o epoch atual, achou %d", attempts, countCurrentEpoch)
	}
}

// TestFencing_TwoConcurrentPromotesOnlyOneEpochWins prova que promover o
// mesmo shard duas vezes ao mesmo tempo (duas regiões disputando o
// failover) nunca produz dois "epoch 2": só uma promoção vence, a outra
// recebe ErrLeaseNotExpired.
func TestFencing_TwoConcurrentPromotesOnlyOneEpochWins(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	svc := fencing.NewService(pool, fencing.SystemClock{})
	shardID := "shard-test-2"

	const attempts = 20
	var wins, losses int64
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := svc.Promote(ctx, shardID, "region-x", time.Hour)
			if err == nil {
				atomic.AddInt64(&wins, 1)
			} else if errors.Is(err, fencing.ErrLeaseNotExpired) {
				atomic.AddInt64(&losses, 1)
			} else {
				t.Errorf("promote concorrente: erro inesperado: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("esperava exatamente 1 promoção vencedora, teve %d", wins)
	}
	if losses != attempts-1 {
		t.Fatalf("esperava %d promoções rejeitadas, teve %d", attempts-1, losses)
	}

	fence, err := svc.CurrentFence(ctx, shardID)
	if err != nil {
		t.Fatalf("ler fence final: %v", err)
	}
	if fence.Epoch != 1 {
		t.Fatalf("epoch final esperado 1 (só a primeira promoção deveria ter valido), veio %d", fence.Epoch)
	}
}
