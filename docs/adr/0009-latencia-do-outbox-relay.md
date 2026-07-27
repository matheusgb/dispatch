# ADR 0009: intervalo do relay do outbox é 1 segundo, aceito como latência real

## Contexto

O `outboxRelayLoop` em `cmd/delivery-api` publica eventos pendentes a cada
1 segundo. O caminho `created -> ready_for_lunchrush -> offered` atravessa
esse relay duas vezes: uma para o `lunchrush-worker` reagir a
`delivery.created` (e produzir `delivery.ready_for_lunchrush`), outra para
reagir a esse evento e produzir `delivery.offered`. Isso foi descoberto
rodando o LoadGen em modo `-distributed`: uma entrega isolada levou
~3,8s para chegar a `offered`; sob concorrência 8, uma fração passou de
30s (ver `docs/benchmarks/loadgen-tier-3-alta-concorrencia.md`).

## Decisão

Manter o intervalo de 1 segundo neste tier, e documentar a latência
resultante como uma característica real do desenho, não escondê-la atrás
de um timeout maior no LoadGen sem explicação. O `LoadGen` no modo
`-distributed` usa um prazo de espera configurável
(`-ready-wait-seconds`, default 30s) exatamente para tornar essa latência
visível e mensurável em vez de mascarada.

## Alternativas consideradas

- **Reduzir o intervalo do relay para closer a 0 (polling agressivo):**
  rejeitada por agora. Aumentaria a carga de `SELECT ... FOR UPDATE`-like no
  PostgreSQL proporcionalmente, sem evidência de que a latência atual
  importa para algum SLO real deste laboratório.
- **Publicar de forma síncrona, no mesmo request que grava o efeito:**
  rejeitada. Isso é exatamente o que o outbox existe para evitar: acoplar a
  confirmação ao cliente à disponibilidade do broker no meço a operação.
- **LISTEN/NOTIFY do PostgreSQL para acordar o relay sem polling:**
  considerada como melhoria futura, não implementada neste tier por não
  haver evidência de que o polling de 1s seja o gargalo real sob a carga
  medida (ver `docs/benchmarks/tier-3-baseline.md`).

## Consequências

- toda automação que dependa do lunchrush-worder avançar uma entrega
  precisa esperar, não assumir imediatismo: testes e LoadGen fazem polling
  com prazo generoso, nunca uma chamada única seguida de asserção;
- a latência composta (dois hops de relay) é maior que a latência de um
  hop só. Se um cenário de produto realmente precisar de "oferta imediata
  após criação", a solução correta é o `delivery-api` já criar a entrega
  em `ready_for_lunchrush` quando não houver necessidade de um passo de
  triagem manual, não acelerar o relay.

## Status

Aceita.
