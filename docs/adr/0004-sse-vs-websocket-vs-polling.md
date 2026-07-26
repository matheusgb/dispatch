# ADR 0004: SSE com fallback de polling, não WebSocket

## Contexto

O cliente que acompanha uma entrega precisa saber quando a posição muda,
sem reimplementar um protocolo de mensageria completo para isso.

## Decisão

`GET /deliveries/{id}/stream` expõe Server-Sent Events: uma conexão HTTP
de longa duração, um evento por posição aceita, texto simples. O cliente
que não conseguir manter a conexão (proxy que corta stream, rede
instável) cai para `GET /deliveries/{id}/position` em polling.

## Alternativas consideradas

- **WebSocket:** rejeitado neste tier. O tráfego é unidirecional
  (servidor -> cliente); WebSocket adicionaria um protocolo bidirecional,
  handshake próprio e mais estado de conexão para um caso de uso que HTTP
  simples já resolve. Entraria se algum dia o cliente precisasse mandar
  algo de volta na mesma conexão, o que não é o caso aqui.
- **Long polling:** rejeitado. Mais chamadas HTTP, mais overhead de
  handshake por atualização, sem ganho real sobre SSE para este padrão de
  poucos eventos por entrega.

## Consequências

- o broker de SSE (`internal/platform/sse`) é em memória, por processo:
  funciona com uma réplica do `delivery-api`. A partir de mais de uma
  réplica, um cliente conectado à réplica A não recebe eventos publicados
  pela réplica B. Isso é uma limitação conhecida, não escondida: ver
  `tier-2-what-breaks-next.md`. O tier 3 resolve isso com um transporte
  compartilhado (Kafka ou pub/sub do Redis);
- o stream é best-effort: um evento perdido não é reenviado. O cliente
  recupera pelo polling, exatamente como a invariante de tracking já
  permite (posição é eventualmente consistente, não transacional);
- o `http.Server` do `delivery-api` tem `WriteTimeout` de 10s para as
  rotas normais; o handler de stream precisa desligar esse timeout para a
  própria conexão via `http.ResponseController.SetWriteDeadline`, senão o
  servidor corta a conexão de qualquer cliente que fique mais de 10s
  aberto. Isso exigiu que os middlewares (`statusRecorder`) implementassem
  `Unwrap() http.ResponseWriter` para o `ResponseController` conseguir
  alcançar a conexão real por baixo das camadas de log e métricas.

## Status

Aceita.
