# chaos/local

Forma reproduzível e versionada dos cenários de chaos já executados e
documentados manualmente nos tiers 3-4 (`docs/benchmarks/chaos-tier-*.md`).
Cada script segue o mesmo formato do relatório: hipótese, estado estável,
injeção, observação, condição de parada, recuperação, como comentário no
topo do próprio script, e imprime cada etapa durante a execução.

| Script | Cenário | Pré-requisito |
| --- | --- | --- |
| `redis-unavailable.sh` | Redis fora do ar, tracking cai para Postgres | `docker compose --profile app up -d --build` |
| `kafka-paused.sh` | Redpanda pausado, outbox absorve a indisponibilidade | idem |
| `postgres-latency-toxiproxy.sh` | latência alta no Postgres via Toxiproxy | idem, mais Docker acessível para subir o container do Toxiproxy |
| `pod-kill.sh` | matar um pod do `delivery-api` no `kind` | `make helm-up` (cluster `kind-dispatch` com o chart instalado) |

Rode via `make chaos SCENARIO=<nome-sem-.sh>` (ver `Makefile`), ou
diretamente com `bash chaos/local/<script>.sh`.

## O que rodar de verdade nesta sessão encontrou

`redis-unavailable.sh` rodou de verdade contra o `docker compose --profile
app` e reproduziu uma regressão real: com o hostname `redis`
impossível de resolver, o pool de conexões do `go-redis` tentava discar 5
vezes com backoff crescente antes de desistir, travando
`GET /deliveries/{id}/position` por ~10s até o `WriteTimeout` do servidor
matar a conexão **sem nunca responder ao cliente** — o oposto do que
`docs/adr/0003-redis-como-projecao.md` promete (fallback rápido para
PostgreSQL). Corrigido em `internal/tracking/cache.go` com um
`context.WithTimeout` de 750ms em volta de cada chamada ao Redis,
independente do timeout do chamador; o script voltou a confirmar a
hipótese original depois da correção (evidência: rodar o script mostra
`fluxo completo: ... read=200 (ok)` mesmo com o Redis parado).

`kafka-paused.sh` também rodou de verdade: o backlog do outbox levou até
~23s para esvaziar depois do `unpause` (o relay faz poll a cada ~5s, e o
Redpanda ainda reconstituindo liderança de partição logo após o unpause
custou 1-2 ciclos extras) — mais que os poucos segundos que o relatório
original do tier 4 registrou. O script usa uma janela de 30s para não
gerar falso negativo; isso não é uma regressão de código, é uma variável
de ambiente (tempo de retomada do broker) que o relatório original não
tinha capturado com essa precisão.

`postgres-latency-toxiproxy.sh` e `pod-kill.sh` não foram reexecutados
nesta sessão: o primeiro por causa do bug de deadlock conhecido do
Toxiproxy 2.12.0 sob concorrência (mitigado no script ao rodar só
sequencial, mas não vale o risco de repetir sem necessidade); o segundo
porque não havia cluster `kind-dispatch` no ar neste passe (só o
`edge-lab`, de outro laboratório, compartilhando a mesma máquina). Os dois
scripts foram revisados manualmente linha a linha contra os comandos reais
já documentados em `docs/benchmarks/chaos-tier-4.md`.
