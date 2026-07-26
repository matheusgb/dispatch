// Package topics centraliza os nomes de tópicos Kafka usados pelo
// dispatch, para produtor e consumidor nunca divergirem por erro de
// digitação. Ver docs/adr/0006-mapa-de-topicos.md para o mapa completo.
package topics

const (
	// DeliveryEvents carrega todo evento de lifecycle da entrega. Chave de
	// partição: delivery_id, preserva ordem por agregado.
	DeliveryEvents = "dispatch.delivery-events"

	// TrackingPositions carrega cada posição de GPS aceita. Chave de
	// partição: delivery_id. Não passa pela outbox: o tracking-ingest
	// confirma ao cliente só depois do ack do Kafka (ver ADR 0007).
	TrackingPositions = "dispatch.tracking-positions"
)

// Kinds de evento dentro de DeliveryEvents.
const (
	KindDeliveryCreated          = "delivery.created"
	KindDeliveryReadyForDispatch = "delivery.ready_for_dispatch"
	KindDeliveryOffered          = "delivery.offered"
	KindDeliveryAssigned         = "delivery.assigned"
	KindDeliveryDeclined         = "delivery.declined"
	KindDeliveryExpired          = "delivery.expired"
	KindDeliveryPickedUp         = "delivery.picked_up"
	KindDeliveryDelivered        = "delivery.delivered"
)
