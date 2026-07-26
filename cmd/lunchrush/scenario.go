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
	Duration         time.Duration `json:"duration_ns"`
	Err              string        `json:"error,omitempty"`
}

type scenario struct {
	client         *client
	couriers       []string
	courierMu      sync.Mutex
	courierCursor  int
	declineRate    float64
	expireRate     float64
	duplicateRate  float64
	offerTTL       int
	expireOfferTTL int
	seed           int64
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

	if _, err := s.client.markReady(ctx, d.ID); err != nil {
		res.Err = err.Error()
		res.Duration = time.Since(start)
		return res
	}

	roll := rng.Float64()
	switch {
	case roll < s.declineRate:
		res.Outcome, err = s.runDecline(ctx, d.ID)
	case roll < s.declineRate+s.expireRate:
		res.Outcome, err = s.runExpire(ctx, d.ID)
	default:
		res.Outcome, res.AssignRetries, err = s.runCompleted(ctx, d.ID)
	}
	if err != nil {
		res.Err = err.Error()
	}
	res.Duration = time.Since(start)
	return res
}

func (s *scenario) runDecline(ctx context.Context, id string) (outcome, error) {
	if _, err := s.client.offer(ctx, id, s.offerTTL); err != nil {
		return "", err
	}
	if _, err := s.client.decline(ctx, id); err != nil {
		return "", err
	}
	return outcomeDeclined, nil
}

func (s *scenario) runExpire(ctx context.Context, id string) (outcome, error) {
	if _, err := s.client.offer(ctx, id, s.expireOfferTTL); err != nil {
		return "", err
	}
	// O delivery-api recicla ofertas vencidas a cada 5s (ver
	// expireOffersLoop em cmd/delivery-api). Espera o prazo mais uma
	// margem para o loop rodar, sem depender de relógio injetável: isso é
	// deliberadamente um teste de caixa preta.
	deadline := time.Now().Add(time.Duration(s.expireOfferTTL)*time.Second + 8*time.Second)
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

func (s *scenario) runCompleted(ctx context.Context, id string) (outcome, int, error) {
	if _, err := s.client.offer(ctx, id, s.offerTTL); err != nil {
		return "", 0, err
	}

	retries := 0
	var assignErr error
	for attempt := 0; attempt < len(s.couriers); attempt++ {
		courierID := s.nextCourier()
		_, assignErr = s.client.assign(ctx, id, courierID)
		if assignErr == nil {
			if _, err := s.client.pickUp(ctx, id); err != nil {
				return "", retries, err
			}
			if _, err := s.client.deliver(ctx, id); err != nil {
				return "", retries, err
			}
			return outcomeCompleted, retries, nil
		}
		retries++
	}
	return "", retries, fmt.Errorf("nenhum entregador do pool ficou livre: %w", assignErr)
}
