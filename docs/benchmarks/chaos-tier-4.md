# Chaos do tier 4

Quatro cenários, todos executados de verdade contra o cluster `kind`
"lunchrush" (deploy via Helm, ver `docs/adr/0013-helm-em-vez-de-kustomize.md`)
e a infra do `docker compose`, não descritos em texto sem rodar. Formato:
hipótese, estado estável, injeção, observação, condição de parada,
recuperação, aprendizado. Evidência bruta de cada um em
`docs/benchmarks/tier-4-chaos-*-evidencia.txt`. Os cenários A e D foram
reexecutados via `chaos/local/pod-kill.sh` e
`chaos/local/postgres-latency-toxiproxy.sh` numa sessão posterior, ver
`docs/benchmarks/chaos-tier-4-reconfirmacao-2026-07-27.md` (inclui um bug
real de script encontrado e corrigido).

## A. Pod kill de uma réplica do `delivery-api`

**Hipótese:** matar um dos dois pods do `delivery-api` não derruba
nenhuma requisição em andamento contra o `Service`: o `Deployment`
recria o pod e, com `minAvailable: 1` no `PodDisruptionBudget`, o outro
pod continua respondendo o tempo todo.

**Estado estável:** dois pods `Running` e `1/1`, `Service` com dois
endpoints.

**Injeção:** um pod de debug dentro do cluster (`chaos-curl`, mesma rede
que o `Service`, diferente de `kubectl port-forward svc/...`, que abre
túnel direto para um único pod e não exercita o balanceamento do
`Service`) disparou `GET /healthz` a cada 200ms via
`http://delivery-api/healthz`. No meio da rajada,
`kubectl delete pod delivery-api-<um-dos-dois>`.

**Observação:** 100 de 100 requisições responderam `200`. O pod novo
ficou `Running` e `1/1` em ~13s (contido dentro da janela de
`startupProbe`: `failureThreshold: 30` × `periodSeconds: 1`).

**Condição de parada:** nenhuma necessária (nenhuma requisição falhou).

**Recuperação:** automática pelo `Deployment` (`ReplicaSet` mantém o
número desejado de réplicas).

**Aprendizado:** `kubectl port-forward svc/...` **não** é equivalente a
bater no `Service`: ele escolhe um pod uma vez e não segue o
balanceamento; a primeira tentativa deste experimento usou
`port-forward` e todas as requisições falharam quando o pod escolhido
foi exatamente o morto. Rodar o cliente de dentro do cluster contra o
DNS do `Service` é o jeito correto de testar isso.

## B. Falha do Redis com `tracking-projector` lendo via fallback

**Hipótese:** com o Redis fora do ar, `GET /deliveries/{id}/position`
continua respondendo `200` lendo direto do PostgreSQL (mesma garantia já
provada no tier 2, ADR 0003, agora com o serviço rodando no `kind` e o
Redis fora do cluster via `redis-external`).

**Estado estável:** fluxo completo (emitir token, criar entrega, ingerir
posição, ler posição) responde `200` em cada etapa, com o Redis saudável.

**Injeção:** `docker compose stop redis` enquanto `tracking-ingest` e
`tracking-projector` seguiam rodando no `kind`.

**Observação:** o mesmo fluxo completo (criar nova entrega, ingerir nova
posição, ler a posição) respondeu `200` em todas as etapas com o Redis
parado.

**Condição de parada:** nenhuma necessária (nenhuma etapa falhou).

**Recuperação:** `docker compose start redis`; o fluxo completo voltou a
responder `200` sem intervenção manual (cache volta a ser populado no
próximo miss).

**Aprendizado:** confirma em ambiente `kind` real o que o teste de
integração `TestTrackingCache_FallsBackToPostgresWhenRedisIsDown`
(`test/integration/tracking_cache_test.go`) já provava em nível de
unidade: o Redis é projeção, nunca fonte de verdade.

## C. Redpanda pausado: outbox absorve a indisponibilidade

**Hipótese:** com o Redpanda indisponível, `POST /deliveries` continua
aceitando pedidos (o efeito de negócio é o `INSERT` no PostgreSQL dentro
da mesma transação do registro em `outbox_events`; a publicação no Kafka
é responsabilidade de um processo separado, o relay do outbox, que pode
atrasar sem bloquear a escrita).

**Estado estável:** três `POST /deliveries` seguidos respondendo `201`.

**Injeção:** `docker pause lunchrush-redpanda-1` (processo congelado, não
apenas parado, para simular indisponibilidade sem o `TCP RST` imediato
de um `stop`).

**Observação:** três novos `POST /deliveries` continuaram respondendo
`201` com o Redpanda pausado. `select count(*) from outbox_events where
published_at is null` mostrou 6 eventos pendentes (os da rajada dentro e
fora da pausa) acumulados sem publicar.

**Condição de parada:** nenhuma necessária (nenhuma escrita falhou; o
acúmulo de pendências no outbox é o comportamento esperado, não uma
falha).

**Recuperação:** `docker unpause lunchrush-redpanda-1`; em menos de 5s a
contagem de `outbox_events` pendentes caiu para 0 (o relay publicou tudo
que tinha acumulado).

**Aprendizado:** o padrão outbox (já em produção desde o tier 3, ADR
0009) segura a indisponibilidade do backbone de eventos inteiramente do
lado da escrita: o cliente HTTP nunca soube que o Kafka estava fora do
ar.

## D. Latência no PostgreSQL via Toxiproxy, com o `delivery-api` do `kind` atravessando o proxy

**Hipótese:** latência alta no PostgreSQL degrada a latência de
`POST /deliveries`, mas não produz erro: mais lento, não incorreto (mesma
hipótese do tier 2, ADR/chaos `chaos-tier-2.md`, agora com o cliente
sendo o `delivery-api` rodando no `kind`, não um smoke k6 externo).

**Cuidado conhecido:** Toxiproxy 2.12.0 tem um bug de deadlock na API de
controle sob alta concorrência com `reset_peer` (reprodução isolada em
`toxiproxy-repro/` na raiz de `labs`, fora deste repositório, não tocada
aqui). Este experimento rodou **sequencial**, uma requisição de cada vez
(bem abaixo dos ~400 clientes concorrentes que disparam o bug).

**Estado estável:** container `chaos-toxiproxy` (imagem oficial 2.12.0)
na mesma rede do `docker compose`, com um proxy `postgres` encaminhando
`0.0.0.0:15432` para `postgres:5432`, sem nenhum toxic. O
`delivery-api` do `kind` reconfigurado via
`externalInfra.postgres.port=15432` (mesmo mecanismo de
`hostGatewayIP` do ADR 0011: o proxy publica a porta no host, alcançável
pelo `kind` pelo gateway da rede docker). 10 requisições sequenciais de
`POST /deliveries`: latência média ~2,6ms.

**Injeção:** toxic `latency` no proxy `postgres`, `stream: downstream`,
300ms ± 100ms de jitter.

**Observação:** as mesmas 10 requisições sequenciais passaram a levar
entre 1,78s e 4,39s cada — bem mais que os 300-400ms esperados de um
único round trip adicional. Nenhuma requisição falhou (`201` em todas).

**Condição de parada:** nenhuma necessária (sem erro, só latência).

**Recuperação:** toxic removido via API do Toxiproxy; latência voltou a
~2,7ms nas 10 requisições seguintes. API de controle do Toxiproxy
conferida como responsiva depois da remoção (`GET /proxies` respondeu
normalmente), sem sinal do bug de deadlock.

**Aprendizado real, não esperado:** a latência observada (1,8-4,4s) foi
muito maior que um único acréscimo de ~300-400ms por requisição.
`handleCreateDelivery` faz mais de um round trip ao PostgreSQL por
requisição (checagem de idempotência, inserção da entrega, inserção do
evento de outbox, tudo dentro da mesma transação, mais a abertura de
conexão do pool quando não há conexão livre). Cada round trip paga o
toxic de latência de novo; com jitter, alguns caem perto do teto (400ms)
e a soma de 4-6 round trips explica a faixa observada. Isto é uma
propriedade real do desenho (uma transação com múltiplas instruções paga
N vezes a latência de rede até o banco), não um artefato do experimento:
fica registrado como candidato a investigação de
`docs/benchmarks/tier-4-what-breaks-next.md` (agrupar as instruções numa
única viagem de rede quando o driver permitir, ou aceitar o custo como
trade-off de correção transacional).

## O que não foi testado neste tier

- node drain (o `kind` usado aqui é de um nó só; não há para onde
  drenar);
- perda de 30% das réplicas simultaneamente (os cenários acima matam uma
  réplica de cada vez; um teste de perda em lote fica para quando houver
  mais réplicas por workload, hoje 2);
- failover do PostgreSQL (não há réplica configurada; ver
  `docs/limitacoes-simulacao-local.md`, RDS/Aurora Multi-AZ não simulado
  localmente);
- degradação de uma zona de disponibilidade (não há zonas reais neste
  laboratório, ver `docs/limitacoes-simulacao-local.md`);
- rotação de secret e restauração de backup (ficam para uma passada
  futura deste tier, se houver fôlego).
