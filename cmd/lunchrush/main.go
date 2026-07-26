// Comando lunchrush é o simulador de carga semântica do tier 1: cria
// entregadores e entregas contra um delivery-api real, decide o desfecho de
// cada uma por seed reproduzível e verifica, em caixa preta, que
// idempotência e disputa de atribuição se comportam como o roadmap exige.
// O k6 continua sendo o teste black-box de HTTP; o LunchRush é o que
// conhece o domínio o suficiente para não deixar passar uma dupla
// atribuição ou uma duplicata que virou dois efeitos.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "endereço do delivery-api")
	seed := flag.Int64("seed", 20260717, "seed do gerador determinístico")
	orders := flag.Int("orders", 200, "número de entregas simuladas")
	courierCount := flag.Int("couriers", 20, "tamanho do pool de entregadores")
	concurrency := flag.Int("concurrency", 20, "ordens simuladas em paralelo")
	declineRate := flag.Float64("decline-rate", 0.1, "fração de ofertas recusadas")
	expireRate := flag.Float64("expire-rate", 0.1, "fração de ofertas que expiram")
	duplicateRate := flag.Float64("duplicate-rate", 0.2, "fração de criações repetidas com a mesma chave")
	offerTTL := flag.Int("offer-ttl-seconds", 30, "prazo da oferta para o caminho de aceite")
	expireOfferTTL := flag.Int("expire-offer-ttl-seconds", 2, "prazo curto usado só no caminho de expiração")
	out := flag.String("out", "lunchrush-report", "prefixo dos arquivos de relatório (.json e .md)")
	adminSecret := flag.String("admin-secret", os.Getenv("DISPATCH_ADMIN_SECRET"), "segredo administrativo para emitir o token de tracking")
	flag.Parse()

	ctx := context.Background()
	c := newClient(*baseURL)

	if *adminSecret == "" {
		log.Fatal("admin-secret é obrigatório (flag ou DISPATCH_ADMIN_SECRET): necessário para o token de tracking")
	}
	token, err := c.issueToken(ctx, *adminSecret, "lunchrush")
	if err != nil {
		log.Fatalf("emitir token de tracking: %v", err)
	}

	log.Printf("cadastrando %d entregadores", *courierCount)
	couriers := make([]string, *courierCount)
	for i := 0; i < *courierCount; i++ {
		id, err := c.registerCourier(ctx, fmt.Sprintf("lunchrush-courier-%d", i))
		if err != nil {
			log.Fatalf("cadastrar entregador %d: %v", i, err)
		}
		if err := c.setAvailability(ctx, id, true); err != nil {
			log.Fatalf("disponibilizar entregador %d: %v", i, err)
		}
		couriers[i] = id
	}

	sc := &scenario{
		client:         c,
		token:          token,
		couriers:       couriers,
		declineRate:    *declineRate,
		expireRate:     *expireRate,
		duplicateRate:  *duplicateRate,
		offerTTL:       *offerTTL,
		expireOfferTTL: *expireOfferTTL,
		seed:           *seed,
	}

	log.Printf("simulando %d ordens com concorrência %d (seed=%d)", *orders, *concurrency, *seed)
	started := time.Now()
	results := make([]orderResult, *orders)

	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup
	for i := 0; i < *orders; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = sc.runOrder(ctx, i)
		}(i)
	}
	wg.Wait()

	s := summarize(*seed, *courierCount, started, results)
	if err := writeReport(*out, s); err != nil {
		log.Fatalf("escrever relatório: %v", err)
	}

	fmt.Printf("concluídas=%d declinadas=%d expiradas=%d erros=%d duração=%s\n",
		s.Completed, s.Declined, s.Expired, s.Errors, s.Duration)
	fmt.Printf("relatório em %s.json e %s.md\n", *out, *out)

	if s.Errors > 0 {
		os.Exit(1)
	}
}
