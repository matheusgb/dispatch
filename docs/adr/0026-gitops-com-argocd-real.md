# ADR 0026: GitOps real com ArgoCD contra o chart existente, sem trocar o deploy padrão

## Contexto

O roadmap pede GitOps como próximo passo natural depois de ter um chart
Helm versionado (`deploy/helm/dispatch/`, ADR 0013): hoje o deploy é
imperativo (`scripts/helm-deploy.sh` roda `helm upgrade --install` na mão
ou via CI). Uma rodada anterior documentou ArgoCD como "fora de alcance
por tempo/memória", mas nunca tentou de verdade instalar um ArgoCD real
contra um cluster `kind` local — a única coisa que faltava era rodar até o
fim.

## Decisão

**Provar GitOps real, sem adotar ArgoCD como caminho padrão de deploy
deste laboratório.** `scripts/helm-deploy.sh` continua sendo como este
projeto normalmente sobe o chart (mais simples para um laboratório solo,
sem servidor de CI hospedado rodando `argocd app sync` de verdade). ArgoCD
fica documentado e provado como evidência de como a mesma infraestrutura
(o chart já existente, sem duplicar nada) se comportaria sob GitOps: uma
`Application` (`deploy/argocd/dispatch-application.yaml`) aponta para
`deploy/helm/dispatch` no repositório público
`github.com/matheusgb/dispatch`, com `syncPolicy.automated.selfHeal: true`.

## Evidência real

Ver `docs/runbooks/gitops-argocd.md` para o passo a passo completo e os
outputs de CLI. Resumo:

- Cluster `kind` chamado `dispatch` (mesmo usado por `scripts/kind-deploy.sh`
  e `scripts/helm-deploy.sh`, sem tocar no cluster `edge-lab` do
  laboratório irmão).
- ArgoCD instalado via manifests oficiais
  (`kubectl apply -n argocd --server-side -f .../install.yaml`; `--server-side`
  foi necessário porque o CRD `applicationsets.argoproj.io` excede o
  limite de 262144 bytes da anotação `kubectl.kubernetes.io/last-applied-configuration`
  usada por `kubectl apply` client-side).
- `Application` sincronizou de verdade contra o repositório GitHub público
  (`repoURL: https://github.com/matheusgb/dispatch.git`, sem token: repo
  público, sem credencial nenhuma configurada no ArgoCD), `status.sync.status: Synced`,
  revisão igual ao commit real do `master` no momento do sync.
- **Achado real, não hipotético:** o `argocd-cm` padrão do ArgoCD exclui o
  kind `Endpoints` da lista de recursos observados/aplicados
  (`resource.exclusions`, comentado como "reduz volume de eventos
  observados e ruído na UI"). O chart usa `Endpoints` manuais (sem
  `selector`) para apontar para a infra externa ao cluster (Postgres,
  Redis, Redpanda, dependency-simulator do `docker compose` do host, ver
  `templates/external-infra.yaml` e ADR 0011) — um padrão legítimo e comum
  para referenciar infra fora do cluster, mas que colide com essa exclusão
  padrão. Sem o `Endpoints`, os `Service` ficam sem endereço de destino e
  todo pod que depende de Postgres/Kafka entra em `CrashLoopBackOff`. A
  correção foi editar `resource.exclusions` no `argocd-cm` para manter só
  `EndpointSlice` excluído (que é espelhado automaticamente a partir do
  `Endpoints` pelo `EndpointSliceMirroring controller` do Kubernetes,
  então continua funcionando) e remover `Endpoints` da lista. Depois da
  correção e de um `argocd.argoproj.io/refresh=hard`, os 10 pods do
  chart (`delivery-api`, `dispatch-worker`, `notification-worker`,
  `tracking-ingest`, `tracking-projector`, 2 réplicas cada) ficaram
  `1/1 Running` e a `Application` foi para `Synced`/`Healthy`.
- **Drift real corrigido:** `kubectl scale deployment delivery-api --replicas=7`
  (fora do fluxo GitOps) mudou `status.sync.status` para `OutOfSync` e
  `status.health.status` para `Progressing` imediatamente; o
  `self-heal: true` do `syncPolicy.automated` reverteu para 2 réplicas em
  menos de 5 segundos, sem intervenção manual, confirmado tanto por
  `kubectl get deployment delivery-api -o jsonpath='{.spec.replicas}'`
  quanto pelo evento real do Kubernetes `Scaled down replica set
  delivery-api-866789759 to 2 from 7`.

## O que isso não prova

- **Nenhum servidor de CI hospedado dispara `argocd app sync` de verdade
  neste laboratório** (não há CI rodando continuamente contra o cluster
  `kind`, que só existe enquanto a máquina de desenvolvimento estiver de
  pé): o ArgoCD provado aqui roda contra um cluster efêmero, criado e
  destruído na mesma sessão que fez este experimento, não um cluster de
  longa duração observando o repositório continuamente. `docs/runbooks/gitops-argocd.md`
  documenta os comandos para reproduzir sob demanda.
- **Não migra o fluxo padrão de deploy deste laboratório para GitOps.**
  `scripts/helm-deploy.sh` continua sendo o caminho documentado em
  `docs/passo-a-passo/` e usado pelo `Makefile` (`make helm-up`); ArgoCD
  é evidência de viabilidade, não adoção.

## Alternativas consideradas

- **Documentar como "fora de alcance" de novo:** rejeitada; a única coisa
  que faltava era tempo para instalar e depurar o achado real do
  `resource.exclusions`, não uma dependência de nuvem paga.
- **Trocar `scripts/helm-deploy.sh` por ArgoCD como único caminho:**
  rejeitada, complexidade desnecessária para um laboratório solo sem CI
  hospedado de longa duração.

## Consequências

- quem quiser rodar GitOps de verdade contra este chart tem um caminho
  documentado e testado, incluindo a pegadinha real do `resource.exclusions`
  (que não é óbvia e custaria tempo de novo numa sessão futura sem este
  registro);
- o chart em si não mudou: a mesma fonte de verdade (`deploy/helm/dispatch`)
  serve tanto `helm upgrade --install` direto quanto ArgoCD.

## Status

Aceita.
