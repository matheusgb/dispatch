# Benchmark: breakpoint test do delivery-api

Segue o formato de `docs/templates/benchmark.md`.

## Cenário

`loadtest/k6/breakpoint.js`, executor `ramping-arrival-rate`, contra
`POST /deliveries` do `delivery-api` real (`docker compose --profile app`,
porta 8083), rodando local nesta máquina. Objetivo: achar a taxa de
chegada onde o sistema viola um SLO de forma segura (stop condition via
threshold com `abortOnFail`), não só provar que aguenta uma carga fixa.

## Configuração

Máquina: 32 vCPU, 15Gi RAM (compartilhada com outro laboratório, `edge-lab`,
rodando um cluster `kind` de 3 nós ao mesmo tempo desta execução).
`delivery-api` rodando como container único, sem réplicas, PostgreSQL
único no mesmo `docker compose`. Estágios de arrival rate: 500 → 1500 →
3000 → 5000 → 8000 → 12000 requisições/s alvo, 15-20s cada, `k6` v2.1.0
rodando no mesmo host (não numa máquina de carga separada — ver limitação
abaixo). SLO de corte: `http_req_failed` < 5% ou `http_req_duration`
p(95) > 2000ms, qualquer um aborta o teste inteiro.

## Resultado (Medido)

Nenhum dos dois thresholds estourou dentro da faixa testada — o teste
completou os 5 estágios sem abortar. Saída bruta completa em
`docs/benchmarks/breakpoint-k6-output.txt`. Números da execução registrada:

| Métrica | Valor |
| --- | --- |
| taxa de chegada alvo no último estágio | 12000 req/s |
| throughput real sustentado (`http_reqs`) | ~4930 req/s |
| taxa de erro | 0,00% (0 de 404127) |
| `http_req_duration` p(50) | 2,4ms |
| `http_req_duration` p(90) | 11,84ms |
| `http_req_duration` p(95) | 26,61ms |
| `http_req_duration` max | 993ms |
| `dropped_iterations` (k6 não conseguiu manter a taxa alvo) | 2020 |
| VUs no pico | 1174 |

Uma execução anterior, mesma configuração, registrou p(95)=19,69ms e
max=1,14s — a mesma ordem de grandeza, confirmando que o padrão
(degradação de latência sem erro) é consistente entre execuções, não
ruído de uma única corrida.

## Comparação

O smoke test (`docs/benchmarks/k6-smoke-tier-1.md`/`.json`, 5 VUs fixos)
mede p(95) na casa de poucos ms. Neste breakpoint, a mesma rota mantém
p(95) baixo (~2ms) até a faixa de milhares de req/s, e só degrada
visivelmente (p(95) subindo para a casa de 20-27ms, cauda batendo perto de
1s) nos dois últimos estágios (8000 e 12000 req/s de taxa alvo). Não houve
colapso de taxa de erro em nenhum ponto testado.

## O que isso não prova

**Não achamos o ponto de ruptura de verdade (colapso de erro).** A carga
foi limitada pela própria máquina de teste: `k6` e `delivery-api` disputam
os mesmos 32 vCPUs (mais o `edge-lab` rodando ao lado), e o k6 já não
conseguia manter a taxa de chegada alvo nos estágios finais
(`dropped_iterations` crescendo, VUs saturando em ~1200). Isso significa
que o número real de degradação medido (~5000 req/s sustentados, p95 subindo
para a casa de 20-27ms) reflete o teto desta máquina compartilhada rodando
cliente e servidor juntos, não necessariamente o teto do `delivery-api`
isolado contra uma máquina de carga dedicada. Um teste de breakpoint mais
rigoroso rodaria o `k6` numa máquina separada do serviço sob teste — fora
de alcance deste laboratório local de single-host.

Este resultado também é de uma única réplica do `delivery-api`, um único
Postgres, sem os outros serviços (Kafka, workers) sob carga simultânea:
não representa o sistema completo em produção, só o caminho de escrita
mais simples (`POST /deliveries`).
