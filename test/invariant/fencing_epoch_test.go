//go:build integration

package invariant

import (
	"testing"
	"time"

	"github.com/matheusgb/lunch-rush/internal/fencing"
)

// Invariante do tier 5/6: "epoch nunca regride" (ver docs/tla/LunchRushFencing.tla
// e docs/adr/0018-fencing-lease-e-epoch.md). test/integration/fencing_test.go
// cobre a rejeição do writer com epoch velho (TestFencing_StaleEpochWriterNeverWrites)
// e a disputa concorrente por promoção (TestFencing_TwoConcurrentPromotesOnlyOneEpochWins);
// este teste cobre o ângulo que nenhum dos dois cobre: uma sequência longa
// de promoções alternando de dono, com a fence consultada entre cada uma,
// nunca produz um epoch menor ou igual ao anterior.
func TestInvariant_FencingEpochNeverRegresses(t *testing.T) {
	truncateAll(t)
	ctx := t.Context()

	svc := fencing.NewService(pool, fencing.SystemClock{})
	const shardID = "shard-invariant-epoch"

	var lastEpoch int64
	for round := 0; round < 20; round++ {
		region := "region-a"
		if round%2 == 1 {
			region = "region-b"
		}

		f, err := svc.Promote(ctx, shardID, region, time.Nanosecond)
		if err != nil {
			t.Fatalf("promoção %d falhou: %v", round, err)
		}
		if f.Epoch <= lastEpoch {
			t.Fatalf("invariante violado: epoch não avançou (era %d, veio %d) na rodada %d", lastEpoch, f.Epoch, round)
		}
		lastEpoch = f.Epoch

		current, err := svc.CurrentFence(ctx, shardID)
		if err != nil {
			t.Fatalf("consultar fence atual na rodada %d: %v", round, err)
		}
		if current.Epoch != f.Epoch || current.OwnerRegion != region {
			t.Fatalf("fence consultada (%+v) diverge do resultado da promoção (%+v) na rodada %d", current, f, round)
		}

		// dá tempo da lease de 1ns expirar antes da próxima promoção;
		// sem isso a próxima chamada devolveria ErrLeaseNotExpired.
		time.Sleep(time.Millisecond)
	}
}
