//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/matheusgb/lunch-rush/internal/tracking"
)

func redisAddr() string {
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		return v
	}
	return "localhost:6379"
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestTrackingCache_ReadThroughOnMiss(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupDeliveryForTracking(t, ctx)

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr()})
	defer rdb.Close()
	if err := rdb.FlushAll(ctx).Err(); err != nil {
		t.Fatalf("limpar redis: %v", err)
	}

	repo := tracking.NewRepository(pool)
	cache := tracking.NewCache(repo, rdb, time.Minute, testLogger())

	p := tracking.Position{DeliveryID: deliveryID, Epoch: 1, Sequence: 1, Latitude: -23.5, Longitude: -46.6, RecordedAt: time.Now()}
	if _, err := cache.RecordPosition(ctx, p); err != nil {
		t.Fatalf("gravar posição: %v", err)
	}

	// A escrita já populou o cache; ler direto do Redis deve achar a chave.
	exists, err := rdb.Exists(ctx, "tracking:last:"+deliveryID).Result()
	if err != nil {
		t.Fatalf("checar cache: %v", err)
	}
	if exists == 0 {
		t.Fatalf("cache deveria ter sido populado após a escrita")
	}

	got, err := cache.CurrentPosition(ctx, deliveryID)
	if err != nil {
		t.Fatalf("ler posição via cache: %v", err)
	}
	if got.Sequence != 1 {
		t.Fatalf("sequence = %d, want 1", got.Sequence)
	}
}

// TestTrackingCache_FallsBackToPostgresWhenRedisIsDown prova a razão de
// existir do ADR "Redis como projeção, não fonte de verdade": apontar o
// cache para um endereço inalcançável não pode derrubar a leitura, só
// adicionar latência.
func TestTrackingCache_FallsBackToPostgresWhenRedisIsDown(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()
	deliveryID := setupDeliveryForTracking(t, ctx)

	repo := tracking.NewRepository(pool)
	if _, err := repo.RecordPosition(ctx, tracking.Position{
		DeliveryID: deliveryID, Epoch: 1, Sequence: 1, Latitude: 1, Longitude: 1, RecordedAt: time.Now(),
	}); err != nil {
		t.Fatalf("gravar posição direto no postgres: %v", err)
	}

	unreachable := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  0,
	})
	defer unreachable.Close()
	cache := tracking.NewCache(repo, unreachable, time.Minute, testLogger())

	got, err := cache.CurrentPosition(ctx, deliveryID)
	if err != nil {
		t.Fatalf("leitura deveria cair para o postgres sem erro, got %v", err)
	}
	if got.Sequence != 1 {
		t.Fatalf("sequence = %d, want 1", got.Sequence)
	}
}
