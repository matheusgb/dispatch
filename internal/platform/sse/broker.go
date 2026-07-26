// Package sse implementa um broker de Server-Sent Events em memória, por
// processo. É deliberadamente simples: um único delivery-api ainda é
// suficiente no tier 2. A partir do tier 3, com mais de uma réplica do
// realtime-gateway, o fan-out de eventos passa a precisar de um transporte
// compartilhado (Kafka ou pub/sub do Redis); isto aqui não escala
// horizontalmente, e está documentado como tal.
package sse

import "sync"

type Broker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan []byte]struct{}
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[string]map[chan []byte]struct{})}
}

// Subscribe registra um canal para o tópico (delivery_id). O cancel deve
// ser chamado quando o cliente desconectar, para não vazar o canal.
func (b *Broker) Subscribe(topic string) (ch chan []byte, cancel func()) {
	ch = make(chan []byte, 8)
	b.mu.Lock()
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[chan []byte]struct{})
	}
	b.subscribers[topic][ch] = struct{}{}
	b.mu.Unlock()

	cancel = func() {
		b.mu.Lock()
		delete(b.subscribers[topic], ch)
		if len(b.subscribers[topic]) == 0 {
			delete(b.subscribers, topic)
		}
		b.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// Publish é best-effort: um assinante lento não bloqueia os outros nem quem
// publica. O stream SSE é isso mesmo, por definição, no roadmap deste
// projeto: se uma atualização for perdida, o cliente recupera pelo
// polling da última posição.
func (b *Broker) Publish(topic string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers[topic] {
		select {
		case ch <- data:
		default:
			// assinante lento: descarta em vez de bloquear ou fechar.
		}
	}
}
