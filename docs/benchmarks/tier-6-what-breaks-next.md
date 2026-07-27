# O que quebra depois do tier 6 (e do roadmap inteiro)

Este é o último tier do roadmap (`lunch-rush.md`). Este documento segue o
mesmo formato dos anteriores (`tier-N-what-breaks-next.md`), mas fecha
também com uma seção sobre o que ficaria como próximo limite se o projeto
continuasse além do roadmap original.

## O que este tier realmente prova

- o mesmo artefato (imagem OCI, mesmo digest) roda sem alteração em dois
  ambientes de execução diferentes;
- o mesmo schema PostgreSQL, restaurado por `pg_dump`/`pg_restore`, é
  aceito sem erro em um banco fisicamente separado;
- o protocolo de fencing do tier 5 (`internal/fencing`), sem nenhuma
  alteração, promove a autoridade entre dois bancos fisicamente
  separados, não só entre duas células lógicas no mesmo processo;
- um writer com o epoch antigo é rejeitado 100% das vezes depois da
  promoção, mesmo tendo cruzado a fronteira de "provedor";
- RTO e RPO são medidos, não estimados, para esse cenário local;
- uma dependência compartilhada real (build/imagem) foi revelada e
  documentada, não escondida.

## O que quebra primeiro se a carga ou a escala crescerem

1. **Kafka sem replicação entre os dois stacks.** Cada Redpanda
   (`cloud-a`, `cloud-b`) foi populado de forma independente pela mesma
   carga, não por cópia real de um para o outro. Um failover real de
   tracking (não só do ledger de assignment) exigiria MSK Replicator ou
   equivalente — não implementado neste tier, é a lacuna mais importante
   deixada aberta.
2. **RTO domina por operação manual, não por tamanho de dado.** Os 11,5s
   medidos são quase todo overhead de processo (`docker compose stop`,
   `dropdb`/`createdb`, `pg_restore`) contra um banco com dezenas de
   linhas. Um banco de produção real com gigabytes levaria minutos a
   horas só no `pg_restore`, e isso não foi medido aqui — extrapolação
   linear não seria uma alegação testada.
3. **A ferramenta de failover (`cmd/cloudfailover`) é manual, não um
   runbook automatizado com stop condition e alarme.** Promover a
   autoridade errada por engano (dois operadores promovendo ao mesmo
   tempo) já é impossível pelo protocolo (`ErrLeaseNotExpired`), mas
   nenhuma automação decide *quando* promover — isso continua sendo uma
   decisão humana neste laboratório.
4. **Registry único como ponto único de falha.** O experimento da seção 6
   do baseline mostrou isso na prática: sem um registry por provedor
   (ou replicação de imagem entre registries), as duas "clouds" não são
   tão independentes quanto a rede e o banco separados sugerem.
5. **Observabilidade não duplicada por provedor.** Não há Prometheus/
   Grafana próprios para `cloud-b` nesta sessão; um failover real
   perderia visibilidade justamente no momento em que mais precisaria
   dela.
6. **Helm/Kubernetes não estendido a um segundo cluster.** O chart do
   tier 4 não foi reaplicado a um segundo `kind` (ou equivalente) para
   `cloud-b`; a prova de portabilidade de execução ficou no nível de
   `docker compose`, não de manifests Kubernetes completos rodando nos
   dois lados.

## Achado real durante o fechamento: teste de integração é sensível ao app tier rodando ao lado

Rodar `go test -tags=integration -race ./test/integration/...` contra
`cloud-a` com `delivery-api`/`lunchrush-worker` também em execução (o app
tier normal do `docker compose --profile app`) produziu uma falha
intermitente: `TestOutbox_CrashBeforeMarkRepublishesButInboxDedupsEffect`
esperava publicar 1 evento pendente e encontrou 0. Causa real: o
`lunchrush-worker` real, rodando ao lado, tem seu próprio relay de outbox
consumindo a mesma tabela `outbox_events` do mesmo banco que o teste usa
— ele conseguiu marcar o evento como publicado antes do relay do teste
rodar, então o teste (que espera encontrar o evento ainda pendente) não
achou nada para publicar. Parar `delivery-api`/`lunchrush-worker`/
`tracking-ingest`/`tracking-projector`/`notification-worker` antes de
rodar a suíte de integração eliminou a flakiness (confirmado: `ok` em
27,7s, execução limpa). Isso não é um bug do tier 6, é uma pré-condição
de isolamento que já existia desde o tier 3 (a suíte assume um banco sem
outro consumidor do outbox competindo), só ficou visível agora porque
esta sessão manteve o app tier rodando continuamente por mais tempo que o
normal. Registrado aqui para não repetir a investigação numa sessão
futura: rode `docker compose stop delivery-api lunchrush-worker
tracking-ingest tracking-projector notification-worker` antes de
`make test-integration` contra um ambiente que já tem o app tier de pé.

## Se este laboratório continuasse além do roadmap

Nenhum destes itens é uma pendência do tier 6 (o critério de conclusão do
roadmap não os exige) — são candidatos honestos de "o que eu faria a
seguir", registrados para não sumir:

- réplica lógica contínua (CDC) entre os dois Postgres, para medir RPO
  perto de zero em vez de depender de backup periódico;
- segundo cluster `kind` com o mesmo Helm chart, para testar portabilidade
  de execução no nível de Kubernetes, não só de `docker compose`;
- registry de imagem por provedor, com replicação de imagem depois do
  build único, fechando a lacuna encontrada na seção 6 do baseline;
- automação do runbook de promoção com stop condition e alarme, em vez de
  comandos manuais de operador;
- medir RTO/RPO com um dataset de tamanho realista (milhões de linhas),
  não dezenas.

## Fechamento do roadmap

Os seis tiers de `lunch-rush.md` (monólito modular → produto local operável
→ sistema distribuído com Kafka → plataforma AWS simulada em três zonas →
células multi-região com fencing e TLA+ → portabilidade entre "clouds")
estão implementados e taggeados (`tier-1.0.0` a `tier-6.0.0`). O que cada
tier genuinamente prova, e o que cada um deixa como limitação conhecida
por depender de infraestrutura paga real (EKS, MSK, RDS Multi-AZ,
ElastiCache, um segundo provedor de nuvem de verdade), está registrado
tier a tier em `docs/limitacoes-simulacao-local.md`. Este projeto nunca
alegou ter operado um sistema multi-cloud em produção; alegou, e provou
com execução real, que o protocolo de correção (idempotência, outbox,
fencing) e o artefato (imagem OCI, schema, contratos) sobrevivem a cada
salto de complexidade que o roadmap propôs.
