//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/matheusgb/lunch-rush/internal/platform/inbox"
	"github.com/matheusgb/lunch-rush/internal/platform/kafka"
	"github.com/matheusgb/lunch-rush/internal/platform/outbox"
)

func kafkaBrokers() []string {
	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		return []string{v}
	}
	// 19092 é o listener "external" do Redpanda do docker compose,
	// anunciado como localhost:19092 especificamente para processos
	// rodando no host (testes, LoadGen via `go run`, k6). O listener
	// "internal" (9092) é anunciado como "redpanda:9092" e só resolve de
	// dentro da rede do compose; ver docker-compose.yml e ADR 0011.
	return []string{"localhost:19092"}
}

func truncateOutbox(t *testing.T) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `TRUNCATE outbox_events, consumed_events`); err != nil {
		t.Fatalf("truncar outbox: %v", err)
	}
}

// TestOutbox_RelayPublishesAndMarks cobre a sequência feliz: gravar dentro
// da transação, o relay publicar e marcar como publicado.
func TestOutbox_RelayPublishesAndMarks(t *testing.T) {
	truncateOutbox(t)
	ctx := context.Background()
	topic := "lunchrush.delivery-events"

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	eventID, err := outbox.Enqueue(ctx, tx, uuid.NewString(), topic, "delivery.created", map[string]string{"delivery_id": "delivery-1"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	producer := kafka.NewProducer(kafkaBrokers())
	defer producer.Close()
	relay := outbox.NewRelay(pool, producer, testLogger())

	// Um consumer group lê de todas as partições, então não precisamos
	// saber em qual partição o balanceador por hash colocou a chave.
	groupReader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: kafkaBrokers(),
		Topic:   topic,
		GroupID: "test-outbox-relay-" + eventID,
	})
	defer groupReader.Close()

	published, err := relay.PublishPending(ctx, 10)
	if err != nil {
		t.Fatalf("publish pending: %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}

	var publishedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM outbox_events WHERE event_id = $1`, eventID).Scan(&publishedAt); err != nil {
		t.Fatalf("consultar published_at: %v", err)
	}
	if publishedAt == nil {
		t.Fatalf("published_at deveria estar preenchido")
	}

	// O tópico acumula mensagens de execuções anteriores; ignora qualquer
	// event_id que não seja o desta execução.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		msg, err := groupReader.ReadMessage(readCtx)
		cancel()
		if err != nil {
			t.Fatalf("ler mensagem publicada: %v", err)
		}
		env, err := outbox.DecodeEnvelope(msg.Value)
		if err != nil {
			t.Fatalf("decodificar envelope: %v", err)
		}
		if env.EventID == eventID {
			return
		}
	}
	t.Fatalf("não recebeu a mensagem publicada para o event_id %s dentro do prazo", eventID)
}

// crashingPublisher publica de verdade no Kafka mas simula o relay
// morrendo antes de marcar published_at: quem chama esta publisher direto
// (sem passar por Relay.PublishPending) reproduz exatamente esse cenário.
type directPublisher struct {
	inner *kafka.Producer
}

func (d directPublisher) Publish(ctx context.Context, topic, key string, value []byte) error {
	return d.inner.Publish(ctx, topic, key, value)
}

// TestOutbox_CrashBeforeMarkRepublishesButInboxDedupsEffect é o caso
// obrigatório do backlog do tier 3: o relay publica, "morre" antes de
// marcar (aqui simulado publicando direto e pulando o update), o evento é
// publicado de novo no próximo ciclo, o consumidor recebe duplicata, e o
// inbox garante que o efeito de negócio aconteça uma vez.
func TestOutbox_CrashBeforeMarkRepublishesButInboxDedupsEffect(t *testing.T) {
	truncateOutbox(t)
	ctx := context.Background()
	topic := "lunchrush.delivery-events"

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	eventID, err := outbox.Enqueue(ctx, tx, uuid.NewString(), topic, "delivery.created", map[string]string{"delivery_id": "delivery-2"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	producer := kafka.NewProducer(kafkaBrokers())
	defer producer.Close()

	// "Relay morto": publica direto, sem marcar published_at. O evento
	// continua elegível para o próximo ciclo do relay de verdade.
	body, _ := json.Marshal(outbox.Envelope{EventID: eventID, Kind: "delivery.created"})
	if err := (directPublisher{inner: producer}).Publish(ctx, topic, eventID, body); err != nil {
		t.Fatalf("publicar direto (simulando o relay antes de morrer): %v", err)
	}

	var publishedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM outbox_events WHERE event_id = $1`, eventID).Scan(&publishedAt); err != nil {
		t.Fatalf("consultar published_at: %v", err)
	}
	if publishedAt != nil {
		t.Fatalf("published_at não deveria estar preenchido antes do relay real rodar")
	}

	// O relay real roda depois do "crash": encontra o evento ainda
	// pendente e publica de novo. Duas mensagens no tópico para o mesmo
	// event_id, isso é esperado (at-least-once).
	relay := outbox.NewRelay(pool, producer, testLogger())
	published, err := relay.PublishPending(ctx, 10)
	if err != nil {
		t.Fatalf("publish pending: %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}

	groupReader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: kafkaBrokers(),
		Topic:   topic,
		GroupID: "test-outbox-crash-" + eventID,
	})
	defer groupReader.Close()

	// O tópico acumula mensagens de execuções anteriores deste teste, e
	// este consumer group novo começa do offset mais antigo: filtramos
	// pelo event_id desta execução para contar só as duas cópias que nos
	// interessam, ignorando qualquer mensagem antiga de outro event_id.
	effectCount := 0
	matchesSeen := 0
	deadline := time.Now().Add(20 * time.Second)
	for matchesSeen < 2 && time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		msg, err := groupReader.ReadMessage(readCtx)
		cancel()
		if err != nil {
			t.Fatalf("ler mensagem: %v", err)
		}
		env, err := outbox.DecodeEnvelope(msg.Value)
		if err != nil {
			t.Fatalf("decodificar envelope: %v", err)
		}
		if env.EventID != eventID {
			continue // mensagem de outra execução do teste, ignora.
		}
		matchesSeen++
		processed, err := inbox.Once(ctx, pool, "test-consumer", env.EventID, func(ctx context.Context) error {
			effectCount++
			return nil
		})
		if err != nil {
			t.Fatalf("inbox.Once: %v", err)
		}
		if matchesSeen == 1 && !processed {
			t.Fatalf("primeira entrega deveria processar o efeito")
		}
		if matchesSeen == 2 && processed {
			t.Fatalf("segunda entrega (duplicata) não deveria reprocessar o efeito")
		}
	}
	if matchesSeen != 2 {
		t.Fatalf("esperava ver 2 cópias do evento %s, viu %d", eventID, matchesSeen)
	}
	if effectCount != 1 {
		t.Fatalf("efeito de negócio executou %d vezes, want exatamente 1", effectCount)
	}
}

func TestInbox_DedupesRepeatedEventID(t *testing.T) {
	truncateOutbox(t)
	ctx := context.Background()
	eventX := uuid.NewString()
	count := 0
	fn := func(ctx context.Context) error { count++; return nil }

	processed1, err := inbox.Once(ctx, pool, "consumer-a", eventX, fn)
	if err != nil || !processed1 {
		t.Fatalf("primeira chamada: processed=%v err=%v", processed1, err)
	}
	processed2, err := inbox.Once(ctx, pool, "consumer-a", eventX, fn)
	if err != nil || processed2 {
		t.Fatalf("segunda chamada deveria pular: processed=%v err=%v", processed2, err)
	}
	// Consumidor diferente processa o mesmo evento independentemente.
	processed3, err := inbox.Once(ctx, pool, "consumer-b", eventX, fn)
	if err != nil || !processed3 {
		t.Fatalf("consumidor diferente deveria processar: processed=%v err=%v", processed3, err)
	}
	if count != 2 {
		t.Fatalf("efeito executou %d vezes, want 2 (um por consumidor)", count)
	}
}
