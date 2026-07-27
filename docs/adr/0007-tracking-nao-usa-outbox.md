# ADR 0007: tracking não passa pela outbox

## Contexto

O outbox garante que um evento só existe se o efeito de domínio que ele
descreve foi commitado no PostgreSQL primeiro. Isso tem um custo: uma
escrita no banco antes de qualquer publicação. GPS é o caminho de escrita
mais quente do sistema (lunch-rush.md, roadmap do tier 3), e cada ponto de
GPS não é, por si só, uma decisão de negócio que precise de durabilidade
transacional imediata como uma atribuição precisa.

## Decisão

`tracking-ingest` publica direto no Kafka (`lunchrush.tracking-positions`),
sem escrever no PostgreSQL e sem outbox. A confirmação ao cliente
(`202 Accepted`) só acontece depois do ack durável do Kafka
(`RequiredAcks: all` em `internal/platform/kafka.Producer`). Se o broker
não confirmar, o endpoint falha explicitamente (`503`) e o cliente retenta
com o mesmo `(tracking_session_epoch, sequence)`, que é idempotente do
lado do `tracking-projector` (constraint única em `tracking_positions`).

## Alternativas consideradas

- **GPS também com outbox:** rejeitada. Adicionaria uma escrita síncrona no
  PostgreSQL ao caminho mais quente do sistema sem nenhum ganho: o
  histórico de GPS já tolera atraso e duplicata por desenho (invariante 7 e
  8), diferente do lifecycle da entrega, onde a ordem de eventos como
  "atribuída" e "entregue" importa para quem consome.

## Consequências

- entre o ack do Kafka e o `tracking-projector` processar a mensagem, existe
  uma janela onde a posição existe no log mas ainda não é visível em
  `GET /deliveries/{id}/position`. Isso é esperado: tracking é eventualmente
  consistente por invariante, não por acidente de implementação;
  `docs/benchmarks/tier-3-baseline.md` mede essa janela;
- se o `tracking-projector` cair, GPS continua sendo aceito e persistido no
  Kafka: nada se perde, só a leitura atrasa até o consumidor voltar. Isso é
  o oposto do outbox, que protege a escrita, mas resolve o mesmo problema
  de durabilidade por outro caminho, adequado a este dado.

## Status

Aceita.
