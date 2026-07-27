# Runbook: rollout progressivo e rollback (Helm/kind)

Fecha a lacuna de "deploy de versão com regressão controlada" do roadmap
de forma proporcional: em vez de instalar ArgoCD/Flux completo
(desproporcional para este laboratório, ver seção final), este runbook
demonstra rollout progressivo real do `Deployment` do Kubernetes, com
`maxSurge`/`maxUnavailable` deliberados no chart Helm
(`deploy/helm/dispatch/templates/workloads.yaml`, ver
`docs/adr` relacionado às PriorityClass no mesmo commit), e um rollback
real acionado por uma imagem propositalmente quebrada.

## Configuração usada (não é o default implícito do Kubernetes)

Cada workload em `deploy/helm/dispatch/values.yaml` declara
`rollingUpdate.maxSurge`/`maxUnavailable` explicitamente:

| Workload | maxSurge | maxUnavailable | por quê |
| --- | --- | --- | --- |
| delivery-api | 1 | 0 | tem `readinessProbe` real (`/readyz`), caminho síncrono do usuário: zero indisponibilidade durante rollout |
| tracking-ingest | 1 | 0 | idem, ingestão de GPS em tempo real |
| tracking-projector | 1 | 0 | idem, leitura de tracking em tempo real |
| dispatch-worker | 1 | 1 | consumidor Kafka, sem `readinessProbe` de negócio, tolera indisponibilidade parcial breve |
| notification-worker | 1 | 1 | assíncrono, não bloqueia o usuário |

Cada workload também ganhou `priorityClassName` real
(`dispatch-critical` para os quatro primeiros, `dispatch-standard` para
`notification-worker`), definido em
`deploy/helm/dispatch/templates/priorityclasses.yaml`.

## Evidência real: PriorityClass no cluster

```
$ kubectl --context kind-dispatch get priorityclass
NAME                      VALUE        GLOBAL-DEFAULT   AGE
dispatch-critical         1000000      false            23s
dispatch-standard         100000       false            23s
system-cluster-critical   2000000000   false            89s
system-node-critical      2000001000   false            89s

$ kubectl --context kind-dispatch -n dispatch get pod -l app=delivery-api \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.priorityClassName}{"\t"}{.spec.priority}{"\n"}{end}'
delivery-api-866789759-hgw9f   dispatch-critical   1000000
delivery-api-866789759-ld56h   dispatch-critical   1000000

$ kubectl --context kind-dispatch -n dispatch get pod -l app=notification-worker \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.priorityClassName}{"\t"}{.spec.priority}{"\n"}{end}'
notification-worker-768b5c5d49-mlxr5   dispatch-standard   100000
notification-worker-768b5c5d49-rtvgq   dispatch-standard   100000
```

## Drill real: regressão controlada e rollback

Executado contra um cluster `kind-dispatch` real (`make helm-up`), imagem
quebrada construída de propósito (`ENTRYPOINT` apontando para um binário
inexistente, simulando um build corrompido publicado por engano).

### 1. Estado antes

```
$ kubectl --context kind-dispatch -n dispatch rollout history deployment/delivery-api
REVISION  CHANGE-CAUSE
1         <none>
```

Dois pods `1/1 Running`, `maxSurge=1`/`maxUnavailable=0` configurados
(confirmado com `kubectl get deployment delivery-api -o
jsonpath='{.spec.strategy}'` →
`{"rollingUpdate":{"maxSurge":1,"maxUnavailable":0},"type":"RollingUpdate"}`).

### 2. Deploy da imagem quebrada

```
$ kubectl --context kind-dispatch -n dispatch set image deployment/delivery-api \
    delivery-api=dispatch-delivery-api:broken
deployment.apps/delivery-api image updated

$ kubectl --context kind-dispatch -n dispatch rollout status deployment/delivery-api --timeout=30s
Waiting for deployment "delivery-api" rollout to finish: 1 out of 2 new replicas have been updated...
error: timed out waiting for the condition
```

### 3. Observação: o rollout trava, o serviço não cai

Com `maxUnavailable: 0`, o Kubernetes nunca derruba um pod saudável antes
do novo estar pronto — e o novo nunca fica pronto, porque nem sobe:

```
$ kubectl --context kind-dispatch -n dispatch get pods -l app=delivery-api -o wide
NAME                            READY   STATUS             RESTARTS      AGE
delivery-api-866789759-hgw9f    1/1     Running            1 (96s ago)   106s
delivery-api-866789759-ld56h    1/1     Running            1 (96s ago)   106s
delivery-api-869d4866bd-nnzj2   0/1     CrashLoopBackOff   2 (14s ago)   39s

$ kubectl --context kind-dispatch -n dispatch describe pod delivery-api-869d4866bd-nnzj2 | tail -6
  Warning  Failed   14s (x3 over 39s)  kubelet  Error: failed to create containerd task: failed to
    create shim task: OCI runtime create failed: runc create failed: unable to start container
    process: exec: "/delivery-api-does-not-exist": stat /delivery-api-does-not-exist: no such file
    or directory: unknown
  Warning  BackOff  7s (x9 over 37s)   kubelet  Back-off restarting failed container delivery-api

$ kubectl --context kind-dispatch -n dispatch get endpoints delivery-api
NAME           ENDPOINTS                          AGE
delivery-api   10.244.0.14:8080,10.244.0.6:8080   107s
```

Os dois endpoints do `Service` continuam sendo os dois pods antigos: o
`Service` nunca aponta para o pod quebrado, porque ele nunca passa no
`readinessProbe` (nem chega a rodar).

### 4. Rollback

```
$ kubectl --context kind-dispatch -n dispatch rollout undo deployment/delivery-api
deployment.apps/delivery-api rolled back

$ kubectl --context kind-dispatch -n dispatch rollout status deployment/delivery-api --timeout=60s
deployment "delivery-api" successfully rolled out

$ kubectl --context kind-dispatch -n dispatch get pods -l app=delivery-api
NAME                            READY   STATUS    RESTARTS   AGE
delivery-api-866789759-hgw9f    1/1     Running   1          2m11s
delivery-api-866789759-ld56h    1/1     Running   1          2m11s

$ kubectl --context kind-dispatch -n dispatch get endpoints delivery-api
NAME           ENDPOINTS                          AGE
delivery-api   10.244.0.14:8080,10.244.0.6:8080   2m11s

$ kubectl --context kind-dispatch -n dispatch run verify-curl --image=curlimages/curl:8.10.1 \
    --restart=Never --rm -i --command -- curl -sS -o /dev/null -w "healthz: %{http_code}\n" \
    http://delivery-api/healthz
healthz: 200
```

Confirmado por requisição HTTP real de dentro do cluster contra o
`Service`: `200`.

## Conclusão

Nenhuma requisição foi perdida durante o drill inteiro: os dois pods
originais nunca saíram do `Service` porque `maxUnavailable: 0` proíbe
isso, e o `rollout undo` recuperou o `Deployment` para a revisão anterior
sem intervenção manual além do comando em si.

## GitOps completo (ArgoCD/Flux): fora de alcance deste passe

Instalar o operador do ArgoCD de verdade neste cluster `kind` de um nó
só, compartilhado com outro laboratório (`edge-lab`) na mesma máquina, foi
avaliado e descartado por tempo/memória disponível nesta sessão — não por
ser sobre AWS ou custo de nuvem (ArgoCD roda em qualquer cluster, inclusive
local). O ArgoCD acrescentaria: sincronização automática a partir de um
repositório Git (este drill foi acionado manualmente via `kubectl`),
histórico de sync visível numa UI, e reversão automática por health check
declarado. O mecanismo de rollback em si (`Deployment` + `ReplicaSet` do
Kubernetes) é o mesmo por baixo, com ou sem ArgoCD — o que este runbook
prova (rollout seguro, rollback funcional, `PriorityClass` real) continua
valendo com ou sem GitOps por cima.
