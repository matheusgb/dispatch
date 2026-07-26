# Tier 3 passo a passo

Continuação do [tier 2](tier-2.md). Aqui o sistema fica realmente
distribuído: Kafka (Redpanda), outbox, inbox, e cinco serviços em vez de
um. Você precisa do que o tier 2 já pedia, mais `kind` e `kubectl`
(instale com `go install sigs.k8s.io/kind@latest` e o método oficial do
seu sistema para `kubectl`).

---

## Passo 1: subir a stack completa (infra + cinco serviços)

```bash
docker compose --profile app --profile observability up -d --build
```

**O que você vai ver:** onze containers, incluindo `redpanda` (saudável
depois de alguns segundos), `redpanda-topics` (roda e sai, cria os
tópicos), e os cinco serviços: `delivery-api`, `dispatch-worker`,
`tracking-ingest`, `tracking-projector`, `notification-worker`.

**O que roda por baixo:** cinco `Dockerfile` novos em
[deploy/compose/](../../deploy/compose/), o mesmo padrão multi-stage
distroless do tier 2. `redpanda-topics` existe só para criar os tópicos
com o número certo de partições antes de qualquer serviço tentar produzir
ou consumir.

---

## Passo 2: ver a jornada completa acontecer sozinha

```bash
BASE=http://localhost:8083
DELIVERY=$(curl -s -X POST $BASE/deliveries -H "X-Caller: order-service" -H "Idempotency-Key: passo-2")
DELIVERY_ID=$(echo "$DELIVERY" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
echo "delivery=$DELIVERY_ID"

# espere alguns segundos e consulte de novo
sleep 5
curl -s $BASE/deliveries/$DELIVERY_ID
```

**O que você vai ver:** a entrega nasce em `created` e, sem nenhuma outra
chamada sua, chega em `offered` sozinha em alguns segundos.

**O que roda por baixo:**
[cmd/delivery-api](../../cmd/delivery-api) grava o evento
`delivery.created` na mesma transação da criação
(`internal/platform/outbox`). Um relay publica esse evento no Kafka a cada
1 segundo. [cmd/dispatch-worker](../../cmd/dispatch-worker) consome
`dispatch.delivery-events`, vê `delivery.created`, chama
`MarkReadyForDispatch`, que grava outro evento
(`delivery.ready_for_dispatch`); o mesmo worker consome esse evento e
chama `Offer`. Dois hops pelo relay, cada um até 1s, mais o processamento:
por isso uma entrega isolada leva uns 3-4 segundos até `offered` (ver ADR
0009).

---

## Passo 3: aceitar, coletar, entregar, e ver a notificação sair sozinha

```bash
COURIER=$(curl -s -X POST $BASE/couriers -H "Content-Type: application/json" -d '{"name":"passo-3"}')
COURIER_ID=$(echo "$COURIER" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
curl -s -X POST $BASE/couriers/$COURIER_ID/availability -H "Content-Type: application/json" -d '{"available":true}'

curl -s -o /dev/null -w "assign: %{http_code}\n" -X POST $BASE/deliveries/$DELIVERY_ID/assign -H "Content-Type: application/json" -d "{\"courier_id\":\"$COURIER_ID\"}"
curl -s -o /dev/null -w "pickup: %{http_code}\n" -X POST $BASE/deliveries/$DELIVERY_ID/pickup
curl -s -o /dev/null -w "deliver: %{http_code}\n" -X POST $BASE/deliveries/$DELIVERY_ID/deliver
```

**O que você vai ver:** os três `204`. Nenhuma chamada para o
`dependency-simulator` aconteceu no seu terminal, mas ela aconteceu:

```bash
docker compose logs notification-worker --tail 20
```

**O que roda por baixo:**
[cmd/notification-worker](../../cmd/notification-worker) consome os
mesmos eventos de lifecycle, filtra por `delivery.assigned` e
`delivery.delivered`, e chama o `dependency-simulator` de forma
assíncrona. Se você chamasse `/assign` no tier 2, a notificação acontecia
dentro do mesmo request; aqui ela saiu do caminho síncrono.

---

## Passo 4: GPS por um serviço, leitura por outro

```bash
TOKEN=$(curl -s -X POST $BASE/auth/tokens -H "X-Admin-Secret: compose-dev-admin-secret" -H "Content-Type: application/json" -d '{"caller":"order-service"}' | python3 -c "import json,sys;print(json.load(sys.stdin)['token'])")

curl -s -X POST http://localhost:8084/deliveries/$DELIVERY_ID/positions -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"tracking_session_epoch":1,"sequence":1,"latitude":-23.55,"longitude":-46.63}'

sleep 1
curl -s http://localhost:8085/deliveries/$DELIVERY_ID/position -H "Authorization: Bearer $TOKEN"
```

**O que você vai ver:** `202` na porta 8084 (`tracking-ingest`), e a
posição de volta na porta 8085 (`tracking-projector`), cerca de 1 segundo
depois.

**O que roda por baixo:**
[cmd/tracking-ingest](../../cmd/tracking-ingest) não escreve em nenhum
banco: só publica no Kafka e confirma depois do ack durável (ADR 0007).
[cmd/tracking-projector](../../cmd/tracking-projector) consome, escreve em
PostgreSQL e Redis, e é quem responde a leitura.

---

## Passo 5: poison pill

```bash
echo '{"not":"a valid envelope"' | docker exec -i dispatch-redpanda-1 rpk topic produce dispatch.delivery-events --key poison-1
sleep 2
docker exec dispatch-redpanda-1 rpk topic consume dispatch.delivery-events.dlq -n 1
docker compose ps dispatch-worker
```

**O que você vai ver:** a mensagem malformada aparece na DLQ, e o
`dispatch-worker` continua `Up` normalmente. Ver
`docs/runbooks/dlq-replay.md`.

---

## Passo 6: subir no kind e provar que roda igual

```bash
bash scripts/kind-deploy.sh
```

**O que você vai ver:** um cluster `kind` de um nó, as cinco imagens
construídas e carregadas, e todos os Deployments com rollout concluído.

**O que roda por baixo:** [scripts/kind-deploy.sh](../../scripts/kind-deploy.sh)
constrói cada imagem, carrega no cluster, descobre o IP de gateway da rede
Docker do `kind` (para alcançar a infra do compose) e aplica
[deploy/kubernetes/base](../../deploy/kubernetes/base) via Kustomize: probes,
`resources`, `securityContext` sem root, `NetworkPolicy` deny-by-default,
`PodDisruptionBudget` e HPA por CPU em `delivery-api` e `tracking-ingest`.

Repita os passos 2 a 4 com `kubectl port-forward`:

```bash
kubectl --context kind-dispatch -n dispatch port-forward svc/delivery-api 18080:80 &
```

---

## Passo 7: réplicas de consumer além das partições

```bash
kubectl --context kind-dispatch -n dispatch scale deployment/dispatch-worker --replicas=4
sleep 10
docker exec dispatch-redpanda-1 rpk group describe dispatch-worker
```

**O que você vai ver:** o grupo com mais membros do que partições
(`dispatch.delivery-events` tem 3), mas só 3 atribuídos a uma partição na
tabela. Os demais existem e não processam nada. Ver ADR 0010.

```bash
kubectl --context kind-dispatch -n dispatch scale deployment/dispatch-worker --replicas=2
```

---

## Passo 8: desligar

```bash
kind delete cluster --name dispatch
docker compose --profile app --profile observability down
```

---

## Resumo da ópera

O tier 3 prova que dá pra distribuir sem perder nem duplicar efeito: o
outbox garante que o evento só existe se o efeito já comitou, o relay
republica sem medo depois de um crash simulado, e o inbox do consumidor
absorve a duplicata que o at-least-once do Kafka garante. O preço foi
latência (dois hops pelo relay antes de `offered`) e duas dores de rede
bem específicas de simular localmente (DNS cruzado entre `kind` e docker
compose, `runAsNonRoot` com imagem distroless) — nenhuma delas veio da
lógica de domínio, que continuou correta o tempo todo.
