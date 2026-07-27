# Benchmark: título

Segue o formato de `docs/benchmarks/tier-N-baseline.md`, use como
referência.

## Cenário

O que foi exercitado (endpoint, fluxo completo, ferramenta de carga: k6,
LoadGen), contra qual ambiente (local, docker compose, kind).

## Configuração

VUs/concorrência, duração, seed, hardware (CPU/memória da máquina que
rodou), para o número fazer sentido fora de contexto.

## Resultado (Medido)

Números reais da execução: p50/p95/p99, taxa de erro, throughput. Sempre
"Medido", nunca "Meta" a menos que seja um alvo explícito documentado à
parte do resultado real.

## Comparação

Se houver baseline anterior do mesmo cenário, compare e explique a
diferença (regressão, melhoria, ruído).

## O que isso não prova

Limite honesto do experimento: hardware de laboratório, sem tráfego
concorrente de outros serviços, etc.
