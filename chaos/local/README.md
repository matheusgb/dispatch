# chaos/local

Estes scripts reproduzem, de forma versionada, os cenários de chaos que já
foram executados e documentados manualmente nos tiers 3 e 4
(`docs/benchmarks/chaos-tier-*.md`). Cada script segue o mesmo formato do
relatório original: hipótese, estado estável, injeção, observação, condição
de parada, recuperação. Isso fica como comentário no topo do próprio script,
e cada etapa é impressa durante a execução.

| Script | Cenário | Pré-requisito |
| --- | --- | --- |
| `redis-unavailable.sh` | Redis fora do ar, tracking cai para Postgres | `docker compose --profile app up -d --build` |
| `kafka-paused.sh` | Redpanda pausado, outbox absorve a indisponibilidade | idem |
| `postgres-latency-toxiproxy.sh` | latência alta no Postgres via Toxiproxy | idem, mais Docker acessível para subir o container do Toxiproxy |
| `pod-kill.sh` | matar um pod do `delivery-api` no `kind` | `make helm-up` (cluster `kind-lunchrush` com o chart instalado) |

Rode via `make chaos SCENARIO=<nome-sem-.sh>` (ver `Makefile`), ou
diretamente com `bash chaos/local/<script>.sh`.

## O que a execução real nesta sessão encontrou

`redis-unavailable.sh` rodou de verdade contra o `docker compose --profile
app` e reproduziu uma regressão real. Com o hostname `redis` impossível de
resolver, o pool de conexões do `go-redis` tentava discar 5 vezes com
backoff crescente antes de desistir. Isso travava
`GET /deliveries/{id}/position` por cerca de 10s, até o `WriteTimeout` do
servidor matar a conexão sem nunca responder ao cliente. É o oposto do que
`docs/adr/0003-redis-como-projecao.md` promete (fallback rápido para
PostgreSQL). A correção foi em `internal/tracking/cache.go`, com um
`context.WithTimeout` de 750ms em volta de cada chamada ao Redis,
independente do timeout do chamador. Depois da correção, o script voltou a
confirmar a hipótese original: rodar o script mostra
`fluxo completo: ... read=200 (ok)` mesmo com o Redis parado.

`kafka-paused.sh` também rodou de verdade. O backlog do outbox levou até
cerca de 23s para esvaziar depois do `unpause`. O relay faz poll a cada ~5s,
e o Redpanda, ainda reconstituindo liderança de partição logo após o
unpause, custou de 1 a 2 ciclos extras. Isso é mais do que os poucos
segundos que o relatório original do tier 4 registrou. O script usa uma
janela de 30s para não gerar falso negativo. Não é uma regressão de código:
é uma variável de ambiente (tempo de retomada do broker) que o relatório
original não tinha capturado com essa precisão.

`postgres-latency-toxiproxy.sh` e `pod-kill.sh` não foram reexecutados
nesta sessão. O primeiro por causa do bug de deadlock conhecido do
Toxiproxy 2.12.0 sob concorrência: o script mitiga isso rodando só de forma
sequencial, mas não vale o risco de repetir sem necessidade. O segundo
porque não havia cluster `kind-lunchrush` no ar neste passe (só o
`edge-lab`, de outro laboratório, compartilhando a mesma máquina). Os dois
scripts foram revisados manualmente, linha a linha, contra os comandos
reais já documentados em `docs/benchmarks/chaos-tier-4.md`.
