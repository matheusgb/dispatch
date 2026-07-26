// Comando cloudfailover é a ferramenta de operador do tier 6: reusa
// internal/fencing (a mesma autoridade de epoch e lease do tier 5, que
// promove entre células) para promover a autoridade de dispatch shard
// entre dois "provedores" (cloud-a, cloud-b), cada um com seu próprio
// PostgreSQL, e provar que um writer com o epoch antigo é rejeitado depois
// da promoção. Não é um protocolo novo: é o mesmo internal/fencing do
// tier 5, chamado contra dois bancos físicos diferentes em vez de um só.
//
// Subcomandos, cada um imprime uma linha JSON em stdout (fácil de
// encadear em scripts/cloud-failover-demo.sh e registrar no
// benchmark):
//
//	cloudfailover promote  -db <url> -shard <id> -region <region> -lease <duration>
//	cloudfailover assign   -db <url> -shard <id> -region <region> -epoch <n> -attempts <n>
//	cloudfailover fence    -db <url> -shard <id>
//	cloudfailover seed     -db <url> -n <n>          (cria N deliveries+couriers livres para os testes de assign)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matheusgb/dispatch/internal/fencing"
)

func main() {
	if len(os.Args) < 2 {
		fatalUsage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbURL := fs.String("db", "", "DATABASE_URL do provedor alvo")
	shardID := fs.String("shard", "tier6-crosscloud", "dispatch shard")
	region := fs.String("region", "", "owner_region a promover/usar")
	lease := fs.Duration("lease", 5*time.Second, "duração da lease")
	epoch := fs.Int64("epoch", 0, "epoch que o writer acredita ser o vigente")
	attempts := fs.Int("attempts", 20, "tentativas concorrentes de CreateAssignment")
	n := fs.Int("n", 20, "quantidade de deliveries/couriers livres a semear")
	fs.Parse(os.Args[2:])

	if *dbURL == "" {
		fmt.Fprintln(os.Stderr, "erro: -db é obrigatório")
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	must(err)
	defer pool.Close()
	svc := fencing.NewService(pool, fencing.SystemClock{})

	switch cmd {
	case "seed":
		seed(ctx, pool, *n)
	case "promote":
		promote(ctx, svc, *shardID, *region, *lease)
	case "assign":
		assign(ctx, pool, svc, *shardID, *region, *epoch, *attempts)
	case "fence":
		currentFence(ctx, svc, *shardID)
	default:
		fatalUsage()
	}
}

func fatalUsage() {
	fmt.Fprintln(os.Stderr, "uso: cloudfailover <seed|promote|assign|fence> [flags]")
	os.Exit(2)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// seed cria N pares delivery+courier livres (estado "offered"/disponível)
// para servirem de matéria-prima aos testes de assign. Reaproveita o
// mesmo esquema que test/integration/fencing_test.go já usa.
func seed(ctx context.Context, pool *pgxpool.Pool, n int) {
	type pair struct {
		DeliveryID string `json:"delivery_id"`
		CourierID  string `json:"courier_id"`
	}
	pairs := make([]pair, 0, n)
	for i := 0; i < n; i++ {
		deliveryID := uuid.NewString()
		courierID := uuid.NewString()
		_, err := pool.Exec(ctx, `INSERT INTO deliveries (id, state) VALUES ($1, 'offered')`, deliveryID)
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO couriers (id, name, available) VALUES ($1, $2, true)`, courierID, "courier-"+courierID)
		must(err)
		pairs = append(pairs, pair{DeliveryID: deliveryID, CourierID: courierID})
	}
	printJSON(map[string]any{"seeded": n, "pairs": pairs, "at": time.Now().UTC()})
}

func promote(ctx context.Context, svc *fencing.Service, shardID, region string, lease time.Duration) {
	if region == "" {
		fmt.Fprintln(os.Stderr, "erro: -region é obrigatório para promote")
		os.Exit(2)
	}
	t0 := time.Now()
	f, err := svc.Promote(ctx, shardID, region, lease)
	elapsed := time.Since(t0)
	if err != nil {
		printJSON(map[string]any{"ok": false, "error": err.Error(), "elapsed_ms": elapsed.Milliseconds(), "at": time.Now().UTC()})
		os.Exit(1)
	}
	printJSON(map[string]any{
		"ok":           true,
		"shard_id":     f.ShardID,
		"epoch":        f.Epoch,
		"owner_region": f.OwnerRegion,
		"lease_until":  f.LeaseUntil,
		"elapsed_ms":   elapsed.Milliseconds(),
		"at":           time.Now().UTC(),
	})
}

// assign dispara N tentativas concorrentes de CreateAssignment usando um
// delivery/courier livre por tentativa (consumidos via seed), exatamente
// como TestFencing_StaleEpochWriterNeverWrites, mas apontado para
// -db (pode ser o Postgres de cloud-a ou de cloud-b) e reportando sucesso/
// ErrStaleFence/outro erro.
func assign(ctx context.Context, pool *pgxpool.Pool, svc *fencing.Service, shardID, region string, epoch int64, attempts int) {
	if region == "" {
		fmt.Fprintln(os.Stderr, "erro: -region é obrigatório para assign")
		os.Exit(2)
	}
	// Duas listas independentes, não um cross join: um cross join entre
	// "deliveries livres" e "couriers livres" pode repetir o mesmo
	// delivery_id ou courier_id em mais de uma linha do resultado, o que
	// faria duas goroutines concorrentes competirem pelo mesmo par sem
	// isso ser sobre fencing (seria só uma corrida de teste malformado).
	// Duas listas ordenadas por id e zipadas por índice garantem N pares
	// disjuntos de verdade.
	deliveryRows, err := pool.Query(ctx, `
		SELECT id FROM deliveries
		WHERE state = 'offered' AND id NOT IN (SELECT delivery_id FROM active_assignments)
		ORDER BY id LIMIT $1
	`, attempts)
	must(err)
	var deliveryIDs []string
	for deliveryRows.Next() {
		var id string
		must(deliveryRows.Scan(&id))
		deliveryIDs = append(deliveryIDs, id)
	}
	deliveryRows.Close()

	courierRows, err := pool.Query(ctx, `
		SELECT id FROM couriers
		WHERE available = true AND id NOT IN (SELECT courier_id FROM active_assignments)
		ORDER BY id LIMIT $1
	`, attempts)
	must(err)
	var courierIDs []string
	for courierRows.Next() {
		var id string
		must(courierRows.Scan(&id))
		courierIDs = append(courierIDs, id)
	}
	courierRows.Close()

	type pair struct{ deliveryID, courierID string }
	var pairs []pair
	for i := 0; i < len(deliveryIDs) && i < len(courierIDs); i++ {
		pairs = append(pairs, pair{deliveryID: deliveryIDs[i], courierID: courierIDs[i]})
	}
	if len(pairs) < attempts {
		fmt.Fprintf(os.Stderr, "aviso: só %d pares livres disponíveis (pedido %d); rode seed antes\n", len(pairs), attempts)
		attempts = len(pairs)
	}

	var successes, staleRejections, otherErrors int64
	var wg sync.WaitGroup
	t0 := time.Now()
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		p := pairs[i]
		go func() {
			defer wg.Done()
			_, err := svc.CreateAssignment(ctx, shardID, region, epoch, p.deliveryID, p.courierID, 0)
			switch {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case err == fencing.ErrStaleFence:
				atomic.AddInt64(&staleRejections, 1)
			default:
				atomic.AddInt64(&otherErrors, 1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(t0)

	printJSON(map[string]any{
		"shard_id":         shardID,
		"region":           region,
		"epoch":            epoch,
		"attempts":         attempts,
		"successes":        successes,
		"stale_rejections": staleRejections,
		"other_errors":     otherErrors,
		"elapsed_ms":       elapsed.Milliseconds(),
		"at":               time.Now().UTC(),
	})
}

func currentFence(ctx context.Context, svc *fencing.Service, shardID string) {
	f, err := svc.CurrentFence(ctx, shardID)
	if err != nil {
		printJSON(map[string]any{"ok": false, "error": err.Error()})
		os.Exit(1)
	}
	printJSON(map[string]any{
		"ok":           true,
		"shard_id":     f.ShardID,
		"epoch":        f.Epoch,
		"owner_region": f.OwnerRegion,
		"lease_until":  f.LeaseUntil,
	})
}
