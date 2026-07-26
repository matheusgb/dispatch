// Package kafka embrulha github.com/segmentio/kafka-go com as escolhas do
// tier 3: chave de partição por agregado, ack durável na escrita, DLQ e
// inbox no consumidor. Nenhum mecanismo de protocolo é reimplementado.
package kafka

import (
	"context"
	"log/slog"

	kafkago "github.com/segmentio/kafka-go"
)

// Producer publica com RequiredAcks=all: só confirma depois que o broker
// garantir a durabilidade configurada. Não há outbox aqui: quem quiser
// outbox usa internal/platform/outbox por cima deste publisher.
type Producer struct {
	writer *kafkago.Writer
}

func NewProducer(brokers []string) *Producer {
	return &Producer{
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(brokers...),
			Balancer:     &kafkago.Hash{}, // chave -> partição estável, preserva ordem por agregado.
			RequiredAcks: kafkago.RequireAll,
			Async:        false,
		},
	}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, value []byte) error {
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	})
}

func (p *Producer) Close() error {
	return p.writer.Close()
}

// Handler processa uma mensagem já decodificada. Um erro devolvido move a
// mensagem para a DLQ em vez de travar a partição num poison pill.
type Handler func(ctx context.Context, msg kafkago.Message) error

// Consumer é um consumer group simples: uma goroutine, sem pool interno de
// workers. Backpressure vem do próprio Kafka (a leitura seguinte não
// acontece até o handler atual terminar), o que já limita quanto trabalho
// concorrente existe por partição.
type Consumer struct {
	reader *kafkago.Reader
	dlq    *Producer
	logger *slog.Logger
}

func NewConsumer(brokers []string, topic, groupID string, dlq *Producer, logger *slog.Logger) *Consumer {
	return &Consumer{
		reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
			// commits explícitos: só depois que o handler confirma sucesso.
		}),
		dlq:    dlq,
		logger: logger,
	}
}

// Run lê até o contexto ser cancelado. Uma falha do handler manda a
// mensagem para <topic>.dlq com o motivo, e o consumo segue: uma mensagem
// venenosa não bloqueia a partição indefinidamente.
func (c *Consumer) Run(ctx context.Context, handle Handler) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		if err := handle(ctx, msg); err != nil {
			c.logger.Error("mensagem foi para a DLQ", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
			if dlqErr := c.dlq.Publish(ctx, msg.Topic+".dlq", string(msg.Key), msg.Value); dlqErr != nil {
				c.logger.Error("publicar na DLQ também falhou", "error", dlqErr)
			}
		}

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			c.logger.Error("commit de offset", "error", err)
		}
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
