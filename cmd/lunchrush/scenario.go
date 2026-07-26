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
	PositionsCurrent int           `json:"positions_current,omitempty"`
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

	// distributed é true a partir do tier 3: ready_for_dispatch e offered
	// acontecem sozinhos, via dispatch-worker reagindo a eventos, então o
	// LunchRush espera em vez de chamar /ready e /offer manualmente. False
	// mantém o comportamento do tier 1/2, onde o teste aciona as duas.
	distributed      bool
	offerTTL         int
	expireOfferTTL   int
	readyWaitSeconds int
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

	key := fmt.Sprintf("lunchrush-%d-%d", s.seed, index)
	d, _, err := s.client.createDelivery(ctx, "lunchrush", key)
	if err != nil {
		res.Err = err.Error()
		res.Duration = time.Since(start)
		return res
	}
	res.DeliveryID = d.ID

	if rng.Float64() < s.duplicateRate {
		res.DuplicateChecked = true
		replay, _, err := s.client.createDelivery(ctx, "lunchrush", key)
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
		res.Outcome, res.AssignRetries, res.PositionsSent, res.PositionsCurrent, err = s.runCompleted(ctx, d.ID, rng)
	}
	if err != nil {
		res.Err = err.Error()
	}
	res.Duration = time.Since(start)
	return res
}

// waitForOffered espera o dispatch-worker mover created -> ready_for_dispatch
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
	return errors.New("dispatch-worker não moveu a entrega para offered dentro do prazo")
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
		// dispatch-worker (OFFER_TTL_SECONDS), não um valor por chamada:
		// o teste precisa saber esperar por esse prazo, não escolhê-lo.
		if err := s.waitForOffered(ctx, id); err != nil {
			return "", err
		}
		waitSeconds = s.readyWaitSeconds
	} else if _, err := s.client.offer(ctx, id, s.expireOfferTTL); err != nil {
		return "", err
	}

	// O dispatch-worker (ou delivery-api no modo monólito) recicla ofertas
	// vencidas periodicamente. Espera o prazo mais uma margem, sem
	// depender de relógio injetável: isso é deliberadamente um teste de
	// caixa preta.
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for time.Now().Before(deadline) {
		d, err := s.client.getDelivery(ctx, id)
		if err != nil {
			return "", err
		}
		if d.State == "ready_for_dispatch" {
			return outcomeExpired, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", errors.New("oferta não expirou dentro do prazo esperado")
}

func (s *scenario) runCompleted(ctx context.Context, id string, rng *rand.Rand) (outcome, int, int, int, error) {
	if err := s.ensureOffered(ctx, id); err != nil {
		return "", 0, 0, 0, err
	}

	retries := 0
	var assignErr error
	for attempt := 0; attempt < len(s.couriers); attempt++ {
		courierID := s.nextCourier()
		_, assignErr = s.client.assign(ctx, id, courierID)
		if assignErr == nil {
			sent, current, err := s.sendPositions(ctx, id, rng)
			if err != nil {
				return "", retries, sent, current, err
			}
			if _, err := s.client.pickUp(ctx, id); err != nil {
				return "", retries, sent, current, err
			}
			if _, err := s.client.deliver(ctx, id); err != nil {
				return "", retries, sent, current, err
			}
			return outcomeCompleted, retries, sent, current, nil
		}
		retries++
	}
	return "", retries, 0, 0, fmt.Errorf("nenhum entregador do pool ficou livre: %w", assignErr)
}

// sendPositions simula o entregador se deslocando: um pequeno trajeto de
// pontos crescentes em epoch/sequence, sempre monotônico. Como a partir do
// tier 3 a ingestão (tracking-ingest) e a projeção (tracking-projector)
// são serviços diferentes ligados por Kafka, a confirmação não é mais
// síncrona: o LunchRush manda os pontos e espera, com um poll curto, a
// projeção alcançar a última sequência enviada. Isso é o LunchRush
// conhecendo o domínio: k6 não saberia dizer se a projeção avançou de
// verdade ou só aceitou a requisição.
func (s *scenario) sendPositions(ctx context.Context, id string, rng *rand.Rand) (sent, current int, err error) {
	epoch := 1
	baseLat, baseLon := -23.55+rng.Float64()*0.05, -46.63+rng.Float64()*0.05
	const steps = 3
	for i := 1; i <= steps; i++ {
		lat := baseLat + float64(i)*0.001
		lon := baseLon + float64(i)*0.001
		if _, err := s.client.recordPosition(ctx, s.token, id, epoch, i, lat, lon); err != nil {
			return sent, current, err
		}
		sent++
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, sequence, err := s.client.currentPosition(ctx, s.token, id)
		if err != nil {
			return sent, current, err
		}
		if status == 200 && sequence == steps {
			current = steps
			return sent, current, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return sent, current, fmt.Errorf("projeção não alcançou a sequência %d dentro do prazo", steps)
}
