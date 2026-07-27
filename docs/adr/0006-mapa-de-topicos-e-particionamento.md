# ADR 0006: mapa de tópicos e chave de partição

## Contexto

O tier 3 precisa de tópicos por responsabilidade, não por tabela, e uma
chave de partição que preserve ordem por agregado sem criar hot partitions
óbvias no volume deste laboratório.

## Decisão

Dois tópicos de negócio, cada um com 3 partições:

| Tópico | Conteúdo | Chave de partição |
| --- | --- | --- |
| `lunchrush.delivery-events` | todo evento de lifecycle (criada, pronta, ofertada, atribuída, recusada, expirada, coletada, entregue) | `delivery_id` |
| `lunchrush.tracking-positions` | cada posição de GPS aceita | `delivery_id` |

Cada um tem uma DLQ irmã (`<tópico>.dlq`) para mensagens que o consumidor
não conseguiu processar (ver `internal/platform/kafka`).

`delivery_id` como chave preserva ordem por entrega (o que importa: uma
entrega não pode processar "delivered" antes de "assigned"), aceitando que
entregas diferentes podem, se caírem na mesma partição, competir por
throughput. Com o volume deste laboratório (LoadGen na casa de dezenas a
centenas de entregas simultâneas), 3 partições por tópico não mostrou
nenhum sinal de partição quente nos testes; a decisão será revisitada com
evidência de carga real, não antes.

## Alternativas consideradas

- **Um tópico por tipo de evento** (`delivery.created`, `delivery.offered`,
  ...): rejeitada. Seria mais granular, mas quebraria a ordenação entre
  eventos da mesma entrega sem um mecanismo extra de sincronização entre
  tópicos.
- **Chave de partição por região ou por entregador:** rejeitada por agora.
  Não há noção de região neste tier (chega no tier 5), e particionar por
  entregador não ajuda a preservar a ordem que importa (a da entrega).

## Consequências

- réplicas úteis de um consumer group são limitadas a 3 (o número de
  partições): a 4ª réplica de `lunchrush-worker` ficaria ociosa. Isso é
  testado no HPA do tier 3 (ver `docs/adr/0010-consumer-replicas-limitadas-por-particoes.md`);
  aumentar as partições é uma migração, não um parâmetro de runtime, então
  o número inicial (3) foi escolhido para caber no ambiente local sem
  forçar um re-particionamento cedo demais.

## Status

Aceita.
