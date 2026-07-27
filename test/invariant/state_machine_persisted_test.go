//go:build integration

package invariant

import (
	"context"
	"testing"
	"time"

	"github.com/matheusgb/lunch-rush/internal/courier"
	"github.com/matheusgb/lunch-rush/internal/delivery"
	"github.com/matheusgb/lunch-rush/internal/lunchrush"
)

// Invariantes 3 e 4 do README: "uma transição de estado só ocorre a partir
// de um estado permitido" e "um estado terminal nunca retorna a um estado
// anterior". internal/delivery/state_test.go já prova isso para a função
// pura Transition (tabela + 100 mil random walks + fuzz); este teste prova
// um ângulo que aquele não cobre: que o *log de transições persistido no
// Postgres* por uma jornada completa via internal/lunchrush é, ele mesmo,
// um caminho válido no mesmo grafo, sem pular etapa e sem duplicar entrada.
func TestInvariant_PersistedTransitionLogFollowsStateMachine(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	couriers := courier.NewRepository(pool)
	c, err := couriers.Register(ctx, "entregador-jornada-completa")
	if err != nil {
		t.Fatalf("cadastrar entregador: %v", err)
	}
	if err := couriers.SetAvailability(ctx, c.ID, true); err != nil {
		t.Fatalf("disponibilizar entregador: %v", err)
	}

	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}

	svc := lunchrush.NewService(pool, lunchrush.FixedClock{At: time.Now()})
	if err := svc.MarkReadyForLunchRush(ctx, created.ID); err != nil {
		t.Fatalf("ready_for_lunchrush: %v", err)
	}
	if err := svc.Offer(ctx, created.ID, time.Hour); err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := svc.Assign(ctx, created.ID, c.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := svc.PickUp(ctx, created.ID); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if err := svc.Deliver(ctx, created.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT from_state, to_state FROM delivery_transitions
		WHERE delivery_id = $1 ORDER BY occurred_at, id
	`, created.ID)
	if err != nil {
		t.Fatalf("consultar log de transições: %v", err)
	}
	defer rows.Close()

	var log []delivery.State
	var prevTo delivery.State
	first := true
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			t.Fatalf("scan: %v", err)
		}
		fromState, toState := delivery.State(from), delivery.State(to)

		if !first && fromState != prevTo {
			t.Fatalf("invariante violado: log descontínuo, transição %s->%s não parte de onde a anterior terminou (%s)", from, to, prevTo)
		}
		if err := delivery.Transition(fromState, toState); err != nil {
			t.Fatalf("invariante violado: transição persistida %s->%s não é permitida pela máquina de estados: %v", from, to, err)
		}
		if fromState.Terminal() {
			t.Fatalf("invariante violado: transição registrada saindo de estado terminal %s", from)
		}

		log = append(log, fromState, toState)
		prevTo = toState
		first = false
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar log de transições: %v", err)
	}

	wantPath := []delivery.State{
		delivery.Created, delivery.ReadyForLunchRush,
		delivery.ReadyForLunchRush, delivery.Offered,
		delivery.Offered, delivery.Assigned,
		delivery.Assigned, delivery.PickedUp,
		delivery.PickedUp, delivery.Delivered,
	}
	if len(log) != len(wantPath) {
		t.Fatalf("log de transições tem %d entradas, esperava %d: %v", len(log), len(wantPath), log)
	}
	for i := range wantPath {
		if log[i] != wantPath[i] {
			t.Fatalf("log de transições diverge em [%d]: got %s want %s (log completo: %v)", i, log[i], wantPath[i], log)
		}
	}

	var finalState string
	if err := pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE id = $1`, created.ID).Scan(&finalState); err != nil {
		t.Fatalf("consultar estado final: %v", err)
	}
	if delivery.State(finalState) != delivery.Delivered {
		t.Fatalf("estado final persistido é %s, esperava %s", finalState, delivery.Delivered)
	}
	if !delivery.State(finalState).Terminal() {
		t.Fatalf("invariante violado: estado final %s deveria ser terminal", finalState)
	}
}
