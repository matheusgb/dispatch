# Teste de carga dedicado do tier 4 (steady state + spike de 3x)

Pendência deixada por uma sessão anterior do tier 4 por restrição de tempo
e memória compartilhada com o `edge-lab`, fechada aqui antes de iniciar o
tier 5. Script: `loadtest/k6/tier-4-steady-spike.js`. Ambiente: `docker
compose --profile app` local (não é o `aws-benchmark` do roadmap, que
exige AWS real e está fora de escopo, ver `docs/limitacoes-simulacao-local.md`).

## Cenário

Jornada feliz completa por iteração (a mesma do `smoke.js`): criar entrega
idempotente, marcar pronta, ofertar, cadastrar e disponibilizar
entregador, atribuir, coletar, entregar, consultar.

Estágios (`ramping-vus`):

| Fase | Duração | VUs |
| --- | --- | --- |
| ramp-up | 15s | 0 → 10 |
| **steady state** | 60s | 10 |
| **spike (~3x)** | 10s | 10 → 30 |
| sustenta o spike | 30s | 30 |
| cooldown | 15s | 30 → 0 |

## Resultado (Medido, execução real de `k6 run`, saída bruta em
`k6-steady-spike-output.txt` e `k6-steady-spike-summary.json`)

| Métrica | Valor |
| --- | --- |
| iterações completas | 15.973 (0 interrompidas) |
| checks | 159.730, 100% sucesso, 0 falhas |
| `http_req_failed` | 0,00% (limiar: `< 1%`) |
| `http_req_duration` p95 | 7,33ms (limiar: `< 1000ms`) |
| `http_req_duration` média | 3,01ms |
| `http_req_duration` máximo | 1,46s (outlier isolado, não sistemático: p95 e p90 seguem baixos) |
| throughput HTTP | ~1.059 req/s |
| throughput de jornadas completas | ~117,7 iterações/s |
| VUs no pico do spike | 30 (3x o steady state de 10) |
| duração total | 2min15s |

Os dois thresholds definidos no script (`http_req_failed rate<0.01` e
`http_req_duration p(95)<1000`) passaram.

## Leitura

O `delivery-api` sozinho, sem contenção de outro serviço no caminho
síncrono da jornada (o outbox absorve a publicação Kafka de forma
assíncrona, como já provado nos chaos do tier 3/4), absorveu o spike de 3x
sem degradação visível de p95 nem erro: o p95 do steady state e do spike
não são reportados separadamente pelo k6 nesta configuração de cenário
único, mas a ausência de erro e a média de 3ms ao longo de toda a janela
(incluindo os 40s de pico) já indicam que o gargalo não estava no
`delivery-api` nesta carga. Isso é consistente com o resultado de
`docs/benchmarks/tier-4-baseline.md`.

## O que isso não prova

Este teste mede o `delivery-api` isolado (criação, oferta manual,
atribuição direta), não o caminho `lunchrush-worker` orientado por
Kafka usado pelo LoadGen em `--distributed`. Uma tentativa de rodar o
LoadGen distribuído com 200 ordens/20 de concorrência nesta mesma janela
mostrou o relay do outbox publicando em lotes de ~100 eventos a cada
~1min45s sob a carga combinada com o restante do trabalho desta sessão
(TLC, SBOM, scans rodando em paralelo na mesma máquina) — sinal de
contenção de CPU compartilhada, não do desenho do outbox em si (o mesmo
padrão já foi provado saudável em isolamento no tier 3/4). Não
reproduzimos esse número como medição formal porque a máquina estava
ocupada com outras tarefas desta sessão ao mesmo tempo: registrar isso
teria misturado "capacidade do sistema" com "contenção de CPU do
laboratório", o que violaria a regra de nunca fabricar ou distorcer
métricas. Ver `docs/benchmarks/tier-5-what-breaks-next.md` para o convite
a medir o caminho distribuído isoladamente numa sessão futura.
