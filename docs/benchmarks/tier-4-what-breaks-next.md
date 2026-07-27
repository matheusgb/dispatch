# O que quebra a seguir (tier 4)

1. **Latência composta em transações com múltiplos round trips.** O
   cenário D de chaos (`chaos-tier-4.md`) mediu 1,8-4,4s de latência para
   `POST /deliveries` com apenas 300ms±100ms de latência injetada no
   PostgreSQL: muito mais que um único round trip explicaria.
   `handleCreateDelivery` faz checagem de idempotência, inserção da
   entrega e inserção do evento de outbox como instruções separadas
   dentro da mesma transação; cada uma paga a latência de rede de novo.
   Se latência de banco em produção for uma preocupação real, a alavanca
   é reduzir o número de round trips (agrupar em menos idas ao banco),
   não só aumentar timeout.

2. **`aws_s3_bucket_lifecycle_configuration` não funciona contra este
   LocalStack.** Removido do módulo `storage` (ADR 0012) depois de travar
   `terraform apply` indefinidamente. Qualquer novo recurso Terraform
   adicionado a este laboratório deveria ser testado com um timeout
   explícito antes de ser considerado parte do módulo: o padrão "roda
   `apply` e espera" não é seguro nem contra uma API 100% local.

3. **Terraform e Helm cobrem só uma fração do roadmap do tier 4.** EKS,
   MSK, RDS/Aurora e ElastiCache não têm módulo Terraform (LocalStack
   community não os implementa de forma honesta); o contrato Multi-AZ
   completo do roadmap (load balancer e nós em três zonas reais, MSK com
   ISR distribuído, RDS Multi-AZ com failover exercitado, RPO/RTO
   medidos em zona de verdade) não é alcançável nesta máquina. Ver
   `docs/limitacoes-simulacao-local.md`.

4. **DNS cruzado entre namespaces do KEDA e da aplicação é uma muleta
   nova, categoria já conhecida.** O `Service` `ExternalName` criado em
   `templates/keda-cross-namespace-dns.yaml` (ADR 0014) resolve o
   problema atual, mas qualquer novo `ScaledObject` que precise falar com
   um serviço externo ao cluster (não só o Redpanda) herda a mesma
   necessidade. Não seria um problema com infra gerenciada de verdade ou
   com o operador do KEDA no mesmo namespace da infra.

5. **KEDA rodando numa versão de Kubernetes não suportada oficialmente.**
   O chart `kedacore/keda` (2.20) declara suporte formal a partir do
   Kubernetes 1.33; o `kind` usado aqui roda 1.31. Funcionou na prática
   (ADR 0014), mas qualquer comportamento futuro fora do que foi testado
   aqui deveria ser validado contra uma versão suportada antes de
   qualquer alegação além do que este tier mediu.

6. ~~Nenhum teste de carga dedicado ao tier 4.~~ **Fechado numa segunda
   passada, antes do tier 5**: `k6` com steady state (10 VUs) e spike de
   3x (30 VUs), 0% de erro em ~144 mil requisições, p95 de 7,33ms. Ver
   `docs/benchmarks/tier-4-load/README.md`. O soak completo (várias horas)
   continua não executado, por escolha de prioridade em favor do tier 5.

7. ~~SBOM, scan e assinatura de imagem não executados.~~ **Fechado numa
   segunda passada**: `syft`, `grype` e `cosign` (chave local) rodados
   contra as 5 imagens do `docker compose`. Ver ADR 0016 e
   `docs/benchmarks/supply-chain/`.

8. **Alertmanager não implantado.** As regras de alerta de burn-rate
   (ADR 0015) são avaliadas pelo Prometheus e aparecem em
   `/api/v1/alerts`, mas nenhum roteamento de notificação real (Slack,
   PagerDuty, e-mail) existe neste laboratório. Um alerta "disparando" só
   é visível consultando o Prometheus diretamente.

9. ~~Backup e recuperação distribuída não exercitados neste tier.~~
   **Fechado numa segunda passada**: `pg_dump`/`pg_restore` reais, gap
   medido contra o high-watermark do Kafka, RPO real de 39s nesta
   execução. Achado novo, não esperado: sob a carga combinada desta
   sessão, o relay do outbox manteve 96-99% dos eventos pendentes o tempo
   todo, publicando em lotes muito mais lentos que o ciclo de ~1s
   esperado: candidato a investigação de causa raiz (contenção de CPU do
   laboratório vs. gargalo real do relay). Ver
   `docs/runbooks/backup-e-recuperacao-distribuida.md`.

Nenhum destes itens bloqueia a tag `tier-4.0.0`: os itens A-D do escopo
do tier (Terraform contra LocalStack, Helm, KEDA, chaos reduzido, SLOs
como código) foram cobertos com evidência real; os itens acima são o
mapa do que continua faltando, não uma alegação de tier completo.
