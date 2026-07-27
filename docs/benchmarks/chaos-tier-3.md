# Chaos local do tier 3

## 1. Poison pill em `lunchrush.delivery-events`

**Hipótese:** uma mensagem que o consumidor não consegue decodificar vai
para a DLQ e não bloqueia a partição.

**Injeção:**

```bash
echo '{"not":"a valid envelope"' | docker exec -i lunchrush-redpanda-1 rpk topic produce lunchrush.delivery-events --key poison-1
```

**Observação:** a mensagem apareceu em `lunchrush.delivery-events.dlq`
íntegra (mesmo payload malformado, para diagnóstico). O `lunchrush-worker`
continuou respondendo `/healthz` durante e depois, e processou mensagens
publicadas em seguida sem atraso.

**Recuperação:** nenhuma necessária além de investigar a origem da
mensagem malformada (runbook `docs/runbooks/dlq-replay.md`).

## 2. Réplicas de consumer além do número de partições

**Hipótese:** escalar um consumer group além do número de partições do
tópico não aumenta throughput; a réplica extra fica ociosa.

**Injeção:** `kubectl -n lunchrush scale deployment/lunchrush-worker --replicas=4`
com o cluster já rodando 2 réplicas e o docker compose com mais uma
instância do mesmo serviço competindo pelo mesmo grupo.

**Observação:** `rpk group describe lunchrush-worker` mostrou 5 membros no
grupo, só 3 atribuídos a uma partição (`lunchrush.delivery-events` tem 3
partições). As outras 2 réplicas do `kind` ficaram sem trabalho.
Evidência completa em `consumer-replicas-limitadas-evidencia.txt` e
ADR 0010.

**Recuperação:** `kubectl -n lunchrush scale deployment/lunchrush-worker --replicas=2`.

## 3. DNS cruzado entre `kind` e a infra do docker compose

**Hipótese:** apontar um Service do Kubernetes para um IP fora do cluster
por `ExternalName` funciona da mesma forma que apontar para um nome DNS.

**Observação:** não funciona. `ExternalName` com um IP literal devolve
`server misbehaving` do CoreDNS. Documentado como achado real, não como
chaos planejado: apareceu durante a primeira tentativa de deploy no
`kind` (ver ADR 0011). A correção (Service/Endpoints manuais, e um Service
extra para o nome curto que o Redpanda anuncia de volta) ficou registrada
para qualquer novo consumidor que precise da mesma infra externa.

## O que não foi testado neste tier

- falha de um broker Redpanda com mais de um nó (o Redpanda deste tier é
  um broker único; réplica e ISR ficam para o tier 4, com MSK real ou uma
  simulação equivalente);
- rebalance de consumer group durante processamento ativo de uma mensagem
  grande (o volume deste laboratório não gerou uma janela de rebalance
  longa o suficiente para observar isso de forma confiável);
- restauração de um tópico a partir de um snapshot ou backup do Redpanda.
