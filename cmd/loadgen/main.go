// Comando loadgen é o simulador de carga semântica do tier 1: cria
// entregadores e entregas contra um delivery-api real, decide o desfecho de
// cada uma por seed reproduzível e verifica, em caixa preta, que
// idempotência e disputa de atribuição se comportam como o roadmap exige.
// O k6 continua sendo o teste black-box de HTTP; o LoadGen é o que
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
	trackingURL := flag.String("tracking-url", "", "endereço do tracking-ingest (default: o mesmo de base-url)")
	projectorURL := flag.String("projector-url", "", "endereço do tracking-projector (default: o mesmo de base-url)")
	seed := flag.Int64("seed", 20260717, "seed do gerador determinístico")
	orders := flag.Int("orders", 200, "número de entregas simuladas")
	courierCount := flag.Int("couriers", 20, "tamanho do pool de entregadores")
	concurrency := flag.Int("concurrency", 20, "ordens simuladas em paralelo")
	declineRate := flag.Float64("decline-rate", 0.1, "fração de ofertas recusadas")
	expireRate := flag.Float64("expire-rate", 0.1, "fração de ofertas que expiram")
	duplicateRate := flag.Float64("duplicate-rate", 0.2, "fração de criações repetidas com a mesma chave")
	offerTTL := flag.Int("offer-ttl-seconds", 30, "prazo da oferta para o caminho de aceite")
	expireOfferTTL := flag.Int("expire-offer-ttl-seconds", 2, "prazo curto usado só no caminho de expiração")
	out := flag.String("out", "loadgen-report", "prefixo dos arquivos de relatório (.json e .md)")
	adminSecret := flag.String("admin-secret", os.Getenv("LUNCHRUSH_ADMIN_SECRET"), "segredo administrativo para emitir o token de tracking")
	distributed := flag.Bool("distributed", false, "tier 3+: não chama /ready e /offer manualmente, espera o lunchrush-worker agir sozinho")
	readyWaitSeconds := flag.Int("ready-wait-seconds", 30, "prazo de espera para o lunchrush-worker mover a entrega até offered (modo -distributed); o relay do outbox publica a cada 1s e o caminho created -> offered atravessa o relay duas vezes")

	// Rede e relógio virtuais (tier 5, ver docs/adr/0020). Defaults em 0:
	// nenhuma flag nova muda o comportamento de nenhum tier anterior.
	netDropRate := flag.Float64("net-drop-rate", 0, "fração de posições de GPS perdidas na rede virtual (nunca enviadas)")
	netDelayMs := flag.Int("net-delay-ms", 0, "atraso mínimo, em ms, antes de cada envio de posição")
	netDelayJitterMs := flag.Int("net-delay-jitter-ms", 0, "jitter adicional, em ms, somado a net-delay-ms")
	netDuplicateRate := flag.Float64("net-duplicate-rate", 0, "fração de trajetos com uma posição duplicada")
	netReorderRate := flag.Float64("net-reorder-rate", 0, "fração de trajetos com duas posições adjacentes trocadas de ordem")
	netClockSkewRate := flag.Float64("net-clock-skew-rate", 0, "fração de trajetos que tentam reenviar uma posição antiga ao final, verificando que a posição atual não regride")
	netCrashRate := flag.Float64("net-crash-rate", 0, "fração de trajetos em que o entregador \"reinicia o app\" no meio (nova tracking_session_epoch)")
	flag.Parse()

	ctx := context.Background()
	c := newClient(*baseURL, *trackingURL, *projectorURL)

	if *adminSecret == "" {
		log.Fatal("admin-secret é obrigatório (flag ou LUNCHRUSH_ADMIN_SECRET): necessário para o token de tracking")
	}
	token, err := c.issueToken(ctx, *adminSecret, "loadgen")
	if err != nil {
		log.Fatalf("emitir token de tracking: %v", err)
	}

	log.Printf("cadastrando %d entregadores", *courierCount)
	couriers := make([]string, *courierCount)
	for i := 0; i < *courierCount; i++ {
		id, err := c.registerCourier(ctx, fmt.Sprintf("loadgen-courier-%d", i))
		if err != nil {
			log.Fatalf("cadastrar entregador %d: %v", i, err)
		}
		if err := c.setAvailability(ctx, id, true); err != nil {
			log.Fatalf("disponibilizar entregador %d: %v", i, err)
		}
		couriers[i] = id
	}

	sc := &scenario{
		client:           c,
		token:            token,
		couriers:         couriers,
		declineRate:      *declineRate,
		expireRate:       *expireRate,
		duplicateRate:    *duplicateRate,
		offerTTL:         *offerTTL,
		expireOfferTTL:   *expireOfferTTL,
		seed:             *seed,
		distributed:      *distributed,
		readyWaitSeconds: *readyWaitSeconds,
		net: netFault{
			dropRate:      *netDropRate,
			delayMs:       *netDelayMs,
			delayJitterMs: *netDelayJitterMs,
			duplicateRate: *netDuplicateRate,
			reorderRate:   *netReorderRate,
			clockSkewRate: *netClockSkewRate,
			crashRate:     *netCrashRate,
		},
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
	fmt.Printf("clock_skew_tried=%d clock_skew_safe=%d couriers_crashed=%d positions_dropped=%d\n",
		s.ClockSkewTried, s.ClockSkewSafe, s.CouriersCrashed, s.PositionsDropped)
	fmt.Printf("relatório em %s.json e %s.md\n", *out, *out)

	// Verificador de histórico (invariante 7, posição monotônica): toda
	// tentativa de clock skew tem que ter sido segura. Se alguma não foi,
	// isto é uma violação de invariante sob rede virtual, não um erro de
	// rede comum, e o run precisa falhar como tal.
	if s.ClockSkewSafe < s.ClockSkewTried {
		log.Printf("VIOLAÇÃO DE INVARIANTE: %d de %d tentativas de clock skew regrediram a posição atual",
			s.ClockSkewTried-s.ClockSkewSafe, s.ClockSkewTried)
		os.Exit(1)
	}

	if s.Errors > 0 {
		os.Exit(1)
	}
}
