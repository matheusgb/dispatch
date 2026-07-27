# Runbook: GitOps real com ArgoCD contra `deploy/helm/lunchrush`

Ver `docs/adr/0026-gitops-com-argocd-real.md` para a decisão e o porquê.
Este runbook é o passo a passo reproduzível e a evidência bruta real desta
sessão (2026-07-27), cluster `kind` chamado `lunchrush` (mesmo nome usado
por `scripts/kind-deploy.sh`/`scripts/helm-deploy.sh`, diferente de
`edge-lab`).

## 1. Subir o cluster e a infra externa

```
docker compose --profile app up -d postgres redis redpanda dependency-simulator redpanda-topics migrate
bash scripts/kind-deploy.sh   # ou scripts/helm-deploy.sh; qualquer um cria o cluster "lunchrush" se não existir
kubectl --context kind-lunchrush delete namespace lunchrush   # ArgoCD vai recriar via Helm
```

## 2. Instalar o ArgoCD real

```
kubectl --context kind-lunchrush create namespace argocd
curl -sSL -o /tmp/argocd-install.yaml https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl --context kind-lunchrush apply -n argocd --server-side --force-conflicts -f /tmp/argocd-install.yaml
kubectl --context kind-lunchrush -n argocd wait --for=condition=available deployment --all --timeout=150s
```

`--server-side` é necessário: `kubectl apply` (client-side, com a
anotação `kubectl.kubernetes.io/last-applied-configuration`) falha no CRD
`applicationsets.argoproj.io` com `metadata.annotations: Too long: must
have at most 262144 bytes`. Saída real desta sessão confirmando o erro e
depois o sucesso com `--server-side` está em `/tmp/argocd-install.yaml`
(comando reproduzido, não versionado).

## 3. Corrigir o `resource.exclusions` (achado real desta sessão)

Por padrão, `argocd-cm` exclui os kinds `Endpoints`/`EndpointSlice` da
observação/aplicação ("reduz volume de eventos e ruído na UI"). O chart
`deploy/helm/lunchrush` usa `Endpoints` manuais (sem `selector`) para
apontar `postgres-external`, `redis-external`, `redpanda-external`,
`redpanda` e `dependency-simulator-external` para o gateway da rede
docker do `kind` (ver `templates/external-infra.yaml`, ADR 0011). Sem essa
correção, o ArgoCD sincroniza os `Service` mas nunca cria os `Endpoints`
correspondentes, e todo pod que depende de Postgres/Kafka fica em
`CrashLoopBackOff` por não conseguir resolver o Service (sem endereço de
destino).

```
kubectl --context kind-lunchrush -n argocd patch cm argocd-cm --type merge -p '{"data":{"resource.exclusions":"...\n- apiGroups:\n  - discovery.k8s.io\n  kinds:\n  - EndpointSlice\n..."}}'
kubectl --context kind-lunchrush -n argocd rollout restart statefulset/argocd-application-controller
kubectl --context kind-lunchrush -n argocd rollout status statefulset/argocd-application-controller --timeout=90s
```

(o YAML completo do patch, com os outros grupos de exclusão padrão
preservados, está no histórico de comandos desta sessão; a única mudança
real é remover `Endpoints` da lista, mantendo `EndpointSlice`, que
continua populado automaticamente pelo `EndpointSliceMirroring
controller` do Kubernetes a partir do `Endpoints` real).

## 4. Aplicar a `Application`

```
GATEWAY_IP=$(docker network inspect kind --format '{{ (index .IPAM.Config 0).Gateway }}')
sed "s/HOST_GATEWAY_IP/${GATEWAY_IP}/" deploy/argocd/lunchrush-application.yaml \
  | kubectl --context kind-lunchrush apply -n argocd -f -
kubectl --context kind-lunchrush -n argocd annotate application lunchrush argocd.argoproj.io/refresh=hard --overwrite
```

## 5. Evidência real: sync

```
$ kubectl --context kind-lunchrush -n argocd get application lunchrush -o wide
NAME       SYNC STATUS   HEALTH STATUS   REVISION                                   PROJECT
lunchrush   Synced        Healthy         844ae05cd6170407ec40f42d3566249c9995b321   default

$ kubectl --context kind-lunchrush -n lunchrush get pods
NAME                                   READY   STATUS    RESTARTS   AGE
delivery-api-866789759-6pfzf           1/1     Running   0          8m16s
delivery-api-866789759-xvbw9           1/1     Running   0          8m16s
lunchrush-worker-6d8b86cc5b-d99fs       1/1     Running   0          8m3s
lunchrush-worker-6d8b86cc5b-fmk62       1/1     Running   0          8m3s
notification-worker-768b5c5d49-l8lp4   1/1     Running   0          8m3s
notification-worker-768b5c5d49-nw77m   1/1     Running   0          8m3s
tracking-ingest-6fb4fd5f57-f7np2       1/1     Running   0          10m
tracking-ingest-6fb4fd5f57-sbphg       1/1     Running   0          10m
tracking-projector-6b7d9f6b74-f7v2g    1/1     Running   0          8m3s
tracking-projector-6b7d9f6b74-zrbjt    1/1     Running   0          8m3s
```

`REVISION` bate com o commit real de `master` no GitHub público no
momento do sync (`repoURL: https://github.com/matheusgb/lunch-rush.git`,
sem token nenhum configurado: repositório público).

## 6. Evidência real: drift detectado e corrigido (self-heal)

```
$ kubectl --context kind-lunchrush -n lunchrush scale deployment delivery-api --replicas=7
deployment.apps/delivery-api scaled

$ kubectl --context kind-lunchrush -n lunchrush get deployment delivery-api -o jsonpath='replicas desejadas={.spec.replicas}{"\n"}'
replicas desejadas=7
$ kubectl --context kind-lunchrush -n argocd get application lunchrush -o jsonpath='sync={.status.sync.status} health={.status.health.status}{"\n"}'
sync=OutOfSync health=Progressing

# menos de 5s depois, sem nenhuma intervenção manual:
$ kubectl --context kind-lunchrush -n lunchrush get deployment delivery-api -o jsonpath='{.spec.replicas}'
2
$ kubectl --context kind-lunchrush -n argocd get application lunchrush -o jsonpath='{.status.sync.status}'
Synced

$ kubectl --context kind-lunchrush -n lunchrush get events --sort-by=.lastTimestamp | tail -1
... Normal ScalingReplicaSet deployment/delivery-api Scaled down replica set delivery-api-866789759 to 2 from 7
```

`syncPolicy.automated.selfHeal: true` (`deploy/argocd/lunchrush-application.yaml`)
reverteu o `kubectl scale` manual sozinho: o drift foi detectado
(`OutOfSync`) e corrigido (`Synced`, réplicas de volta a 2, evento real do
Kubernetes confirmando o scale down) em segundos, sem `argocd app sync`
manual.

## 7. Limpeza

Este ArgoCD e o cluster `kind` "lunchrush" usados para este experimento
foram derrubados ao final da sessão que gerou esta evidência:

```
kind delete cluster --name lunchrush
docker compose --profile app down
```

Reproduza a partir do passo 1 sempre que precisar validar de novo; nada
disso fica rodando por padrão neste laboratório.
