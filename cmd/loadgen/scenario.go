package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// outcome é o destino de uma entrega simulada, decidido pela seed antes de
// qualquer chamada de rede: o resultado é reproduzível, mesmo que o tempo de
// cada chamada não seja.
type outcome string

const (
	outcomeCompleted outcome = "completed"
	outcomeDeclined  outcome = "declined"
	outcomeExpired   outcome = "expired"
)

type orderResult struct {
	Index            int           `json:"index"`
	DeliveryID       string        `json:"delivery_id"`
	Outcome          outcome       `json:"outcome"`
	DuplicateChecked bool          `json:"duplicate_checked"`
	DuplicateOK      bool          `json:"duplicate_ok"`
	AssignRetries    int           `json:"assign_retries,omitempty"`
	PositionsSent    int           `json:"positions_sent,omitempty"`
	PositionsDropped int           `json:"positions_dropped,omitempty"`
	PositionsCurrent int           `json:"positions_current,omitempty"`
	CourierCrashed   bool          `json:"courier_crashed,omitempty"`
	ClockSkewTried   bool          `json:"clock_skew_tried,omitempty"`
	ClockSkewSafe    bool          `json:"clock_skew_safe,omitempty"`
	Duration         time.Duration `json:"duration_ns"`
	Err              string        `json:"error,omitempty"`
}

type scenario struct {
	client        *client
	token         string
	couriers      []string
	courierMu     sync.Mutex
	courierCursor int
	declineRate   float64
	expireRate    float64
	duplicateRate float64
	seed          int64

	// distributed é true a partir do tier 3: ready_for_lunchrush e offered
	// acontecem sozinhos, via lunchrush-worker reagindo a eventos, então o
	// LoadGen espera em vez de chamar /ready e /offer manualmente. False
	// mantém o comportamento do tier 1/2, onde o teste aciona as duas.
	distributed      bool
	offerTTL         int
	expireOfferTTL   int
	readyWaitSeconds int

	// net é o relógio e a rede virtuais do tier 5 (docs/tla e
	// docs/adr/0020-loadgen-rede-e-relogio-virtuais.md). Zero value
	// (todas as taxas em 0) reproduz exatamente o comportamento anterior
	// ao tier 5: nenhuma flag nova muda o default de nenhum tier anterior.
	net netFault
}

// nextCourier faz round-robin sobre o pool de entregadores. A rotação é
// protegida por mutex porque várias ordens simuladas correm em paralelo.
func (s *scenario) nextCourier() string {
	s.courierMu.Lock()
	defer s.courierMu.Unlock()
	c := s.couriers[s.courierCursor%len(s.couriers)]
	s.courierCursor++
	return c
}

func (s *scenario) runOrder(ctx context.Context, index int) orderResult {
	start := time.Now()
	rng := rand.New(rand.NewSource(s.seed + int64(index)))
	res := orderResult{Index: index}

	key := fmt.Sprintf("loadgen-%d-%d", s.seed, index)
	d, _, err := s.client.createDelivery(ctx, "loadgen", key)
	if err != nil {
		res.Err = err.Error()
		res.Duration = time.Since(start)
		return res
	}
	res.DeliveryID = d.ID

	if rng.Float64() < s.duplicateRate {
		res.DuplicateChecked = true
		replay, _, err := s.client.createDelivery(ctx, "loadgen", key)
		res.DuplicateOK = err == nil && replay.ID == d.ID
	}

	if !s.distributed {
		if _, err := s.client.markReady(ctx, d.ID); err != nil {
			res.Err = err.Error()
			res.Duration = time.Since(start)
			return res
		}
	}

	roll := rng.Float64()
	switch {
	case roll < s.declineRate:
		res.Outcome, err = s.runDecline(ctx, d.ID)
	case roll < s.declineRate+s.expireRate:
		res.Outcome, err = s.runExpire(ctx, d.ID)
	default:
		var stats positionStats
		res.Outcome, res.AssignRetries, stats, err = s.runCompleted(ctx, d.ID, rng)
		res.PositionsSent = stats.sent
		res.PositionsDropped = stats.dropped
		res.PositionsCurrent = stats.current
		res.CourierCrashed = stats.crashed
		res.ClockSkewTried = stats.skewTried
		res.ClockSkewSafe = stats.skewSafe
	}
	if err != nil {
		res.Err = err.Error()
	}
	res.Duration = time.Since(start)
	return res
}

// waitForOffered espera o lunchrush-worker mover created -> ready_for_lunchrush
// -> offered sozinho. Só é usado quando distributed é true.
func (s *scenario) waitForOffered(ctx context.Context, id string) error {
	deadline := time.Now().Add(time.Duration(s.readyWaitSeconds) * time.Second)
	for time.Now().Before(deadline) {
		d, err := s.client.getDelivery(ctx, id)
		if err != nil {
			return err
		}
		if d.State == "offered" {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("lunchrush-worker não moveu a entrega para offered dentro do prazo")
}

func (s *scenario) ensureOffered(ctx context.Context, id string) error {
	if s.distributed {
		return s.waitForOffered(ctx, id)
	}
	_, err := s.client.offer(ctx, id, s.offerTTL)
	return err
}

func (s *scenario) runDecline(ctx context.Context, id string) (outcome, error) {
	if err := s.ensureOffered(ctx, id); err != nil {
		return "", err
	}
	if _, err := s.client.decline(ctx, id); err != nil {
		return "", err
	}
	return outcomeDeclined, nil
}

func (s *scenario) runExpire(ctx context.Context, id string) (outcome, error) {
	waitSeconds := s.expireOfferTTL + 8
	if s.distributed {
		// O modo distribuído usa o TTL configurado no próprio
		// lunchrush-worker (OFFER_TTL_SECONDS), não um valor por chamada:
		// o teste precisa saber esperar por esse prazo, não escolhê-lo.
		if err := s.waitForOffered(ctx, id); err != nil {
			return "", err
		}
		waitSeconds = s.readyWaitSeconds
	} else if _, err := s.client.offer(ctx, id, s.expireOfferTTL); err != nil {
		return "", err
	}

	// O lunchrush-worker (ou delivery-api no modo monólito) recicla ofertas
	// vencidas periodicamente. Espera o prazo mais uma margem, sem
	// depender de relógio injetável: isso é deliberadamente um teste de
	// caixa preta.
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for time.Now().Before(deadline) {
		d, err := s.client.getDelivery(ctx, id)
		if err != nil {
			return "", err
		}
		if d.State == "ready_for_lunchrush" {
			return outcomeExpired, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", errors.New("oferta não expirou dentro do prazo esperado")
}

type positionStats struct {
	sent, dropped, current       int
	crashed, skewTried, skewSafe bool
}

func (s *scenario) runCompleted(ctx context.Context, id string, rng *rand.Rand) (outcome, int, positionStats, error) {
	if err := s.ensureOffered(ctx, id); err != nil {
		return "", 0, positionStats{}, err
	}

	retries := 0
	var assignErr error
	for attempt := 0; attempt < len(s.couriers); attempt++ {
		courierID := s.nextCourier()
		_, assignErr = s.client.assign(ctx, id, courierID)
		if assignErr == nil {
			stats, err := s.sendPositions(ctx, id, rng)
			if err != nil {
				return "", retries, stats, err
			}
			if _, err := s.client.pickUp(ctx, id); err != nil {
				return "", retries, stats, err
			}
			if _, err := s.client.deliver(ctx, id); err != nil {
				return "", retries, stats, err
			}
			return outcomeCompleted, retries, stats, nil
		}
		retries++
	}
	return "", retries, positionStats{}, fmt.Errorf("nenhum entregador do pool ficou livre: %w", assignErr)
}

// sendPositions simula o entregador se deslocando, sob a rede e o relógio
// virtuais do tier 5 (netfault.go): o trajeto planejado por
// s.net.planPositions pode chegar reordenado, duplicado, com um crash de
// sessão no meio e uma tentativa de clock skew ao final, mas toda posição
// que de fato "chega" (não sorteada como dropped) é uma chamada HTTP real
// contra o tracking-ingest real — o simulador não reimplementa
// monotonicidade nem dedup, só cria as condições de corrida que o domínio
// (internal/tracking) já promete resolver desde o tier 2. Como a partir do
// tier 3 a ingestão e a projeção são serviços diferentes ligados por
// Kafka, a confirmação não é síncrona: o LoadGen manda os pontos e
// espera, com um poll curto, a projeção alcançar a maior sequência
// aceita do epoch final.
func (s *scenario) sendPositions(ctx context.Context, id string, rng *rand.Rand) (positionStats, error) {
	baseLat, baseLon := -23.55+rng.Float64()*0.05, -46.63+rng.Float64()*0.05
	const steps = 3
	planned, dropped, crashed, skewAttempt := s.net.planPositions(rng, baseLat, baseLon, steps)

	stats := positionStats{crashed: crashed}
	finalEpoch, finalSeq := 1, 0
	for i, p := range planned {
		if dropped[i] {
			stats.dropped++
			continue
		}
		if d := s.net.delay(rng); d > 0 {
			time.Sleep(d)
		}
		if _, err := s.client.recordPosition(ctx, s.token, id, p.epoch, p.seq, p.lat, p.lon); err != nil {
			return stats, err
		}
		stats.sent++
		if p.epoch > finalEpoch || (p.epoch == finalEpoch && p.seq > finalSeq) {
			finalEpoch, finalSeq = p.epoch, p.seq
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	reached := false
	for time.Now().Before(deadline) {
		status, sequence, err := s.client.currentPosition(ctx, s.token, id)
		if err != nil {
			return stats, err
		}
		if status == 200 && sequence == finalSeq {
			stats.current = sequence
			reached = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !reached {
		return stats, fmt.Errorf("projeção não alcançou a sequência %d do epoch %d dentro do prazo", finalSeq, finalEpoch)
	}

	// Clock skew: reenvia um ponto do primeiro epoch, sequência 1 — mais
	// antigo que qualquer coisa já confirmada. A invariante 7 (posição
	// monotônica) exige que isto nunca regrida a posição atual.
	if skewAttempt != nil {
		stats.skewTried = true
		if _, err := s.client.recordPosition(ctx, s.token, id, skewAttempt.epoch, skewAttempt.seq, skewAttempt.lat, skewAttempt.lon); err != nil {
			return stats, err
		}
		time.Sleep(300 * time.Millisecond) // dá tempo do projector processar, se for processar
		status, sequence, err := s.client.currentPosition(ctx, s.token, id)
		if err != nil {
			return stats, err
		}
		stats.skewSafe = status == 200 && sequence >= finalSeq
	}

	return stats, nil
}
