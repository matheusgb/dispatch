# Aprendizado do tier 4

1. **O que eu não sabia no início?** Documentos YAML dentro de um `range`
   de template Helm, sem `---` explícito no início de cada iteração, se
   fundem silenciosamente quando o parser encontra chaves repetidas
   (`apiVersion`, `kind`, `metadata`, `spec`). O último valor vence, sem
   erro. Um `HorizontalPodAutoscaler` inteiro desapareceu desse jeito,
   substituído pelo próximo `Deployment` do laço. Nem `helm lint`, nem
   `helm template`, nem `kubectl apply --dry-run=client` detectaram isso.
   Só apareceu ao rodar `kubectl get hpa` contra o cluster real e contar
   zero (ADR 0013).

2. **Que hipótese testei?** Que o padrão outbox, já provado no tier 3
   contra crash do processo, também segura a indisponibilidade completa
   do backbone de eventos, não só um crash do relay. Confirmado: com o
   Redpanda pausado, `POST /deliveries` continuou aceitando escrita, e os
   eventos acumulados em `outbox_events` publicaram todos em menos de 5s
   assim que o broker voltou (cenário C, `chaos-tier-4.md`).

3. **Que evidência mudou minha opinião?** A latência observada no
   cenário de Toxiproxy contra o PostgreSQL (cenário D). Eu esperava algo
   próximo de 300 a 400ms de acréscimo por requisição, que era o toxic
   configurado. Medi 1,8 a 4,4s. A causa real são múltiplos round trips
   síncronos numa mesma transação: `handleCreateDelivery` faz checagem de
   idempotência, inserção da entrega e inserção do evento de outbox como
   instruções separadas. Isso não é um artefato do experimento, é uma
   propriedade real do desenho, que eu não tinha medido antes de ter uma
   fonte de latência de rede alta o suficiente para expor.

4. **Onde o sistema quebrou?** Três vezes, nenhuma em lógica de negócio.
   Primeiro, `terraform apply` travou indefinidamente no recurso
   `aws_s3_bucket_lifecycle_configuration` contra o LocalStack 3.8.1: o
   provider nunca considera a configuração propagada, mesmo com o
   LocalStack respondendo 200 a cada tentativa (ADR 0012). Essa foi
   provavelmente a causa da sessão anterior de trabalho neste tier ter
   travado sem retornar. Segundo, o mesmo problema de DNS cruzado do ADR
   0011 reapareceu numa forma nova: o operador do KEDA, rodando no
   namespace `keda`, não resolvia o nome curto `redpanda` que o broker
   anuncia de volta, porque esse nome só resolve dentro do namespace de
   quem pergunta (ADR 0014). Terceiro, as mensagens de teste que injetei
   no tópico real durante o experimento de KEDA contaminaram os testes de
   integração rodados depois, já que o cluster `kind` e o `docker
   compose` compartilham a mesma infra Kafka. Isso quebrou
   `TestOutbox_RelayPublishesAndMarks` até eu recriar o tópico.

5. **Como diagnostiquei?** O problema do Terraform, batendo o log do
   LocalStack diretamente (`docker logs`) e vendo
   `GetBucketLifecycleConfiguration => 200` repetido indefinidamente sem
   o `apply` nunca terminar. O sintoma no terminal, comando pendurado,
   não dizia nada sobre a causa: só o log do serviço mostrou. O problema
   do KEDA, pelo evento `KEDAScalerFailed` no `kubectl describe
   scaledobject`, com a mensagem de erro completa (`lookup redpanda on
   ...: server misbehaving`) apontando exatamente para o mesmo padrão já
   visto no ADR 0011. A contaminação do tópico, pela mensagem de erro do
   teste (`invalid character 'c' looking for beginning of value`, a letra
   inicial de "chaos-lag-test-junk-N"): o próprio texto da mensagem
   malformada apareceu no erro de decodificação.

6. **Qual solução considerei e rejeitei?** Deixar
   `aws_s3_bucket_lifecycle_configuration` no módulo e só documentar "não
   rode `apply` neste recurso". Rejeitei porque um módulo Terraform com
   uma armadilha escondida é pior que um módulo menor e honesto. O
   recurso foi removido, com a intenção original (expirar versões antigas
   de comprovante) registrada em comentário, não fingida como aplicada.

7. **Que complexidade aceitei?** Um `Service` `ExternalName` extra no
   namespace `keda`, só para resolver um nome curto entre namespaces (ADR
   0014). É a mesma categoria de muleta do ADR 0011, específica de rodar
   infra compartilhada fora do cluster e um operador de autoscaling em um
   namespace separado, não uma decisão de arquitetura do sistema em si.

8. **O que eu faria diferente em um sistema real?** Isolaria a infra de
   teste (tópicos Kafka, banco) de qualquer ambiente usado para
   experimentos exploratórios, como o teste de lag do KEDA. Neste
   laboratório os dois compartilham o mesmo Redpanda por economia de
   recursos da máquina. Num ambiente com mais de um cluster Kafka
   disponível, cada finalidade teria o seu.

9. **Qual é o próximo limite conhecido?** Terraform e Helm neste tier só
   cobrem S3, Secrets Manager/KMS e os workloads da aplicação. EKS, MSK,
   RDS/Aurora e ElastiCache não têm módulo, porque o LocalStack community
   não os implementa. O contrato Multi-AZ do roadmap (load balancer e nós
   em três zonas reais, failover de RDS exercitado) não é alcançável numa
   máquina local só. Ver `docs/limitacoes-simulacao-local.md` e
   `docs/benchmarks/tier-4-what-breaks-next.md`.
