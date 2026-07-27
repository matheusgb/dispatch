//go:build integration

package invariant

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/matheusgb/dispatch/internal/delivery"
)

// authorizedForTracking replica, de propósito, a mesma decisão de
// autorização de cmd/tracking-projector/http.go authorizeOwner (linhas
// ~40-55): só o caller que criou a entrega (created_by_caller) pode
// consultar o tracking dela; dono vazio nunca autoriza ninguém (fail
// closed). authorizeOwner não é exportado por estar em package main de um
// cmd/, então este teste valida o mesmo contrato contra
// internal/delivery.Repository.Owner, que é o dado real que a decisão usa
// — é o ponto onde um bug de autorização apareceria primeiro.
func authorizedForTracking(owner, caller string) bool {
	return owner != "" && owner == caller
}

// Ameaça 1 do docs/security/threat-model.md: "usuário consulta tracking de
// entrega alheia". Nenhum teste automatizado exercitava este limite antes
// desta sessão (auditoria confirmou: zero cobertura de authorizeOwner em
// test/integration/ ou em qualquer outro pacote).
func TestInvariant_CallerNeverAuthorizedForDeliveryTheyDidNotCreate(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	deliveries := delivery.NewRepository(pool)
	created, _, err := deliveries.Create(ctx, delivery.CreateRequest{Caller: "order-service-a", Key: t.Name()})
	if err != nil {
		t.Fatalf("criar entrega: %v", err)
	}

	owner, err := deliveries.Owner(ctx, created.ID)
	if err != nil {
		t.Fatalf("buscar dono: %v", err)
	}
	if owner != "order-service-a" {
		t.Fatalf("dono persistido é %q, esperava %q", owner, "order-service-a")
	}

	if authorizedForTracking(owner, "order-service-a") != true {
		t.Fatalf("invariante violado: o próprio criador deveria ser autorizado")
	}
	if authorizedForTracking(owner, "order-service-b") != false {
		t.Fatalf("invariante violado: um caller diferente do criador não deveria ser autorizado")
	}
	if authorizedForTracking(owner, "") != false {
		t.Fatalf("invariante violado: caller vazio não deveria ser autorizado")
	}
}

func TestInvariant_DeliveryWithoutOwnerAuthorizesNoOne(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	id := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO deliveries (id, state, created_by_caller, created_at, updated_at)
		VALUES ($1, 'created', NULL, now(), now())
	`, id); err != nil {
		t.Fatalf("inserir entrega sem dono: %v", err)
	}

	deliveries := delivery.NewRepository(pool)
	owner, err := deliveries.Owner(ctx, id)
	if err != nil {
		t.Fatalf("buscar dono: %v", err)
	}
	if owner != "" {
		t.Fatalf("dono deveria ser vazio para entrega sem created_by_caller, veio %q", owner)
	}
	if authorizedForTracking(owner, "qualquer-caller") {
		t.Fatalf("invariante violado: entrega sem dono nunca deveria autorizar ninguém (fail closed)")
	}
}
