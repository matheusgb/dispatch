# Runbook: rollout progressivo e rollback (Helm/kind)

Fecha a lacuna de "deploy de versão com regressão controlada" do roadmap
com o mecanismo que sustenta rollout/rollback com ou sem GitOps por cima
(ver seção final para a evidência real de ArgoCD, provada à parte): este
runbook demonstra rollout progressivo real do `Deployment` do Kubernetes, com
`maxSurge`/`maxUnavailable` deliberados no chart Helm
(`deploy/helm/lunchrush/templates/workloads.yaml`, ver
`docs/adr` relacionado às PriorityClass no mesmo commit), e um rollback
real acionado por uma imagem propositalmente quebrada.

## Configuração usada (não é o default implícito do Kubernetes)

Cada workload em `deploy/helm/lunchrush/values.yaml` declara
`rollingUpdate.maxSurge`/`maxUnavailable` explicitamente:

| Workload | maxSurge | maxUnavailable | por quê |
| --- | --- | --- | --- |
| delivery-api | 1 | 0 | tem `readinessProbe` real (`/readyz`), caminho síncrono do usuário: zero indisponibilidade durante rollout |
| tracking-ingest | 1 | 0 | idem, ingestão de GPS em tempo real |
| tracking-projector | 1 | 0 | idem, leitura de tracking em tempo real |
| lunchrush-worker | 1 | 1 | consumidor Kafka, sem `readinessProbe` de negócio, tolera indisponibilidade parcial breve |
| notification-worker | 1 | 1 | assíncrono, não bloqueia o usuário |

Cada workload também ganhou `priorityClassName` real
(`lunchrush-critical` para os quatro primeiros, `lunchrush-standard` para
`notification-worker`), definido em
`deploy/helm/lunchrush/templates/priorityclasses.yaml`.

## Evidência real: PriorityClass no cluster

```
$ kubectl --context kind-lunchrush get priorityclass
NAME                      VALUE        GLOBAL-DEFAULT   AGE
lunchrush-critical         1000000      false            23s
lunchrush-standard         100000       false            23s
system-cluster-critical   2000000000   false            89s
system-node-critical      2000001000   false            89s

$ kubectl --context kind-lunchrush -n lunchrush get pod -l app=delivery-api \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.priorityClassName}{"\t"}{.spec.priority}{"\n"}{end}'
delivery-api-866789759-hgw9f   lunchrush-critical   1000000
delivery-api-866789759-ld56h   lunchrush-critical   1000000

$ kubectl --context kind-lunchrush -n lunchrush get pod -l app=notification-worker \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.priorityClassName}{"\t"}{.spec.priority}{"\n"}{end}'
notification-worker-768b5c5d49-mlxr5   lunchrush-standard   100000
notification-worker-768b5c5d49-rtvgq   lunchrush-standard   100000
```

## Drill real: regressão controlada e rollback

Executado contra um cluster `kind-lunchrush` real (`make helm-up`), imagem
quebrada construída de propósito (`ENTRYPOINT` apontando para um binário
inexistente, simulando um build corrompido publicado por engano).

### 1. Estado antes

```
$ kubectl --context kind-lunchrush -n lunchrush rollout history deployment/delivery-api
REVISION  CHANGE-CAUSE
1         <none>
```

Dois pods `1/1 Running`, `maxSurge=1`/`maxUnavailable=0` configurados
(confirmado com `kubectl get deployment delivery-api -o
jsonpath='{.spec.strategy}'` →
`{"rollingUpdate":{"maxSurge":1,"maxUnavailable":0},"type":"RollingUpdate"}`).

### 2. Deploy da imagem quebrada

```
$ kubectl --context kind-lunchrush -n lunchrush set image deployment/delivery-api \
    delivery-api=lunchrush-delivery-api:broken
deployment.apps/delivery-api image updated

$ kubectl --context kind-lunchrush -n lunchrush rollout status deployment/delivery-api --timeout=30s
Waiting for deployment "delivery-api" rollout to finish: 1 out of 2 new replicas have been updated...
error: timed out waiting for the condition
```

### 3. Observação: o rollout trava, o serviço não cai

Com `maxUnavailable: 0`, o Kubernetes nunca derruba um pod saudável antes
do novo estar pronto, e o novo nunca fica pronto, porque nem sobe:

```
$ kubectl --context kind-lunchrush -n lunchrush get pods -l app=delivery-api -o wide
NAME                            READY   STATUS             RESTARTS      AGE
delivery-api-866789759-hgw9f    1/1     Running            1 (96s ago)   106s
delivery-api-866789759-ld56h    1/1     Running            1 (96s ago)   106s
delivery-api-869d4866bd-nnzj2   0/1     CrashLoopBackOff   2 (14s ago)   39s

$ kubectl --context kind-lunchrush -n lunchrush describe pod delivery-api-869d4866bd-nnzj2 | tail -6
  Warning  Failed   14s (x3 over 39s)  kubelet  Error: failed to create containerd task: failed to
    create shim task: OCI runtime create failed: runc create failed: unable to start container
    process: exec: "/delivery-api-does-not-exist": stat /delivery-api-does-not-exist: no such file
    or directory: unknown
  Warning  BackOff  7s (x9 over 37s)   kubelet  Back-off restarting failed container delivery-api

$ kubectl --context kind-lunchrush -n lunchrush get endpoints delivery-api
NAME           ENDPOINTS                          AGE
delivery-api   10.244.0.14:8080,10.244.0.6:8080   107s
```

Os dois endpoints do `Service` continuam sendo os dois pods antigos: o
`Service` nunca aponta para o pod quebrado, porque ele nunca passa no
`readinessProbe` (nem chega a rodar).

### 4. Rollback

```
$ kubectl --context kind-lunchrush -n lunchrush rollout undo deployment/delivery-api
deployment.apps/delivery-api rolled back

$ kubectl --context kind-lunchrush -n lunchrush rollout status deployment/delivery-api --timeout=60s
deployment "delivery-api" successfully rolled out

$ kubectl --context kind-lunchrush -n lunchrush get pods -l app=delivery-api
NAME                            READY   STATUS    RESTARTS   AGE
delivery-api-866789759-hgw9f    1/1     Running   1          2m11s
delivery-api-866789759-ld56h    1/1     Running   1          2m11s

$ kubectl --context kind-lunchrush -n lunchrush get endpoints delivery-api
NAME           ENDPOINTS                          AGE
delivery-api   10.244.0.14:8080,10.244.0.6:8080   2m11s

$ kubectl --context kind-lunchrush -n lunchrush run verify-curl --image=curlimages/curl:8.10.1 \
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

## GitOps completo (ArgoCD): provado à parte

Uma rodada anterior descartou instalar o ArgoCD de verdade "por
tempo/memória". Sessão seguinte tentou até o fim: ArgoCD real instalado
no cluster `kind` "lunchrush", sincronizando `deploy/helm/lunchrush` a
partir do repositório Git público, com sync e correção de drift reais
provados por comando (não só descritos). Ver `docs/adr/0026-gitops-com-argocd-real.md`
e `docs/runbooks/gitops-argocd.md`. O mecanismo de rollback em si
(`Deployment` + `ReplicaSet` do Kubernetes) é o mesmo por baixo, com ou
sem ArgoCD: o que este runbook prova (rollout seguro, rollback
funcional, `PriorityClass` real) continua valendo com ou sem GitOps por
cima; ArgoCD não substitui `scripts/helm-deploy.sh` como caminho padrão
de deploy deste laboratório (ver ADR 0026 para o porquê).
