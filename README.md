# dispatch

Uma plataforma de última milha precisa encontrar um entregador, acompanhar o
deslocamento e manter cliente e operação informados mesmo sob disputa
concorrente, picos de tráfego e falhas parciais. Este repositório constrói
essa plataforma em Go, começando por um monólito modular correto e evoluindo,
tier por tier, até uma arquitetura celular multi-região com prova formal de
protocolo.

A partir do tier 4, o roadmap original pede AWS real. Este projeto não usa
conta de nuvem paga: veja `docs/limitacoes-simulacao-local.md` para o que é
simulado com ferramentas locais maduras e o que não tem equivalente honesto.

```text
tier atual: 2, concluído
Clientes -> delivery-api -> PostgreSQL (fonte de verdade)
                         -> Redis (projeção descartável de tracking)
                         -> dependency-simulator (notificação)
```

## Invariantes já exigidas até este tier

1. Uma entrega possui no máximo um entregador ativo.
2. Um entregador possui no máximo uma entrega ativa.
3. Uma transição de estado só ocorre a partir de um estado permitido.
4. Um estado terminal nunca retorna a um estado anterior.
5. Repetir uma requisição com a mesma chave de idempotência produz um único
   efeito de negócio.
6. Um comando confirmado como durável não desaparece silenciosamente.
7. Uma posição com sessão ou sequência anterior nunca substitui uma posição
   mais recente.
8. Uma duplicata pode ser reprocessada, mas não duplica o efeito.

Detalhes e a partir de qual tier cada invariante entra:
`docs/requisitos-tier-1.md`.

## Placar de resultados

| Resultado                                    | Valor                     | Relatório |
| --------------------------------------------- | ------------------------- | --------- |
| atribuições concorrentes corretas             | 20 tentativas → 1 vencedora | `docs/benchmarks/tier-1-baseline.md` |
| sequências de estado sem transição inválida   | 100.000 + 18.049.426 (fuzz) | `docs/benchmarks/tier-1-baseline.md` |
| k6 smoke lifecycle                            | 0% de falha, p95 3,61 ms  | `docs/benchmarks/k6-smoke-tier-1.txt` |
| k6 smoke tracking                             | 0% de falha, p95 3,78 ms  | `docs/benchmarks/k6-tracking-tier-2.txt` |
| LunchRush golden path (GPS incluso)           | 0 erros, 72/72 posições aceitas | `docs/benchmarks/lunchrush-tier-2-golden.md` |
| autorização por recurso                       | dono → 200, outro caller → 403, sem token → 401 | `docs/passo-a-passo/tier-2.md` |
| chaos: Redis fora do ar                       | 0 falhas de leitura, latência maior | `docs/benchmarks/chaos-tier-2.md` |
| chaos: delivery-api morto sob carga           | 0 entregadores com dupla atribuição | `docs/benchmarks/chaos-tier-2.md` |
| chaos: 300ms de latência no PostgreSQL        | 0% de falha, p95 de 4,5ms para 1,5s | `docs/benchmarks/chaos-tier-2.md` |

Todos os números acima são **Medido** em ambiente local de desenvolvimento,
não em produção. Os rótulos usados neste repositório são Premissa, Meta e
Medido, nunca um número solto.

## Como executar

```bash
docker compose --profile app --profile observability up -d --build
```

Sobe PostgreSQL, Redis, `delivery-api`, `dependency-simulator`, Prometheus e
Grafana (`http://localhost:3000`, login anônimo local). Se as portas 8080 ou
8090 já estiverem em uso no seu host, o `delivery-api` fica em `8083` e o
`dependency-simulator` em `8092` (ver `docker-compose.yml`).

Testes e carga:

```bash
make test              # unitários
make test-race         # com o detector de corrida
make test-integration  # requer Postgres e Redis locais, já migrados
make load-smoke        # k6, requer o delivery-api no ar
make load-lunchrush    # LunchRush, requer o delivery-api no ar
```

Passo a passo completo, com o que esperar em cada comando:
`docs/passo-a-passo/tier-1.md` e `docs/passo-a-passo/tier-2.md`.

## Estágio atual e próximo gate

Tier 2 concluído: tracking de GPS com monotonicidade por
`(tracking_session_epoch, sequence)`, Redis como projeção descartável com
fallback comprovado, SSE com fallback de polling, autenticação por token
assinado, autorização por recurso, rate limit por caller, notificação
transacional simulada, Docker multi-stage sem root, observabilidade com
Prometheus e Grafana provisionados, e três experimentos de chaos local
documentados.

Não entregue neste tier, por não ser possível nesta forma de trabalho:
demonstração em vídeo. Loki e Tempo ficaram fora por escopo, não por
limite técnico (ver `docs/benchmarks/tier-2-what-breaks-next.md`).

O tier 3 (Kafka, outbox, inbox, separação de serviços, Kubernetes local)
começa a partir daqui.
