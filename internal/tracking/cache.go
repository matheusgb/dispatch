package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// cacheOpTimeout limita cada chamada ao Redis, independente do contexto do
// chamador. Achado real via chaos/local/redis-unavailable.sh (docs/adr/0003):
// com o hostname "redis" impossível de resolver (container parado), o pool
// de conexões do go-redis tentava discar 5 vezes com backoff crescente,
// travando a requisição HTTP por ~10s até o WriteTimeout do servidor matar
// a conexão sem nunca responder ao cliente — pior que um 500 rápido, e
// justamente o oposto do que o fallback para PostgreSQL promete. Um teto
// curto por operação garante que o fallback é rápido de verdade.
const cacheOpTimeout = 750 * time.Millisecond

// Cache é a projeção descartável da última posição em Redis: cache-aside na
// leitura, escrita best-effort depois de um commit confirmado no
// PostgreSQL. O PostgreSQL continua sendo a única fonte de verdade; perder
// o Redis nunca perde uma posição, só adiciona latência até reconstruir o
// cache (ver ADR de Redis como projeção).
type Cache struct {
	repo   *Repository
	rdb    *redis.Client
	ttl    time.Duration
	logger *slog.Logger
}

func NewCache(repo *Repository, rdb *redis.Client, ttl time.Duration, logger *slog.Logger) *Cache {
	return &Cache{repo: repo, rdb: rdb, ttl: ttl, logger: logger}
}

func cacheKey(deliveryID string) string {
	return "tracking:last:" + deliveryID
}

// CurrentPosition tenta o Redis primeiro. Em caso de miss ou de qualquer
// erro do Redis (indisponibilidade, timeout), cai para o PostgreSQL sem
// propagar o erro do cache para o chamador: o cache é acelerador, não
// dependência dura.
func (c *Cache) CurrentPosition(ctx context.Context, deliveryID string) (Position, error) {
	getCtx, cancel := context.WithTimeout(ctx, cacheOpTimeout)
	raw, err := c.rdb.Get(getCtx, cacheKey(deliveryID)).Result()
	cancel()
	switch {
	case err == nil:
		var p Position
		if jsonErr := json.Unmarshal([]byte(raw), &p); jsonErr == nil {
			return p, nil
		}
		// valor corrompido no cache: trata como miss e segue pro Postgres.
	case errors.Is(err, redis.Nil):
		// miss normal, segue pro Postgres.
	default:
		c.logger.Warn("redis indisponível para leitura de tracking, usando postgres", "error", err)
	}

	p, err := c.repo.CurrentPosition(ctx, deliveryID)
	if err != nil {
		return Position{}, err
	}
	c.populate(ctx, p)
	return p, nil
}

// RecordPosition grava no PostgreSQL primeiro; só depois do commit
// confirmado é que o cache é atualizado, e só se esta posição se tornou a
// mais recente. Uma posição atrasada, rejeitada pelo repositório, nunca
// toca o cache.
func (c *Cache) RecordPosition(ctx context.Context, p Position) (bool, error) {
	current, err := c.repo.RecordPosition(ctx, p)
	if err != nil {
		return false, err
	}
	if current {
		c.populate(ctx, p)
	}
	return current, nil
}

func (c *Cache) populate(ctx context.Context, p Position) {
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	setCtx, cancel := context.WithTimeout(ctx, cacheOpTimeout)
	defer cancel()
	if err := c.rdb.Set(setCtx, cacheKey(p.DeliveryID), b, c.ttl).Err(); err != nil {
		c.logger.Warn("redis indisponível para atualizar cache de tracking", "error", err)
	}
}
