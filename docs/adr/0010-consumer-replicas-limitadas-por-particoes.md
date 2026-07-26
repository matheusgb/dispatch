# ADR 0010: réplicas úteis de consumer são limitadas pelas partições

## Contexto

O roadmap do tier 3 pede explicitamente "teste que demonstra que réplicas
úteis de consumer são limitadas pelas partições". `dispatch.delivery-events`
e `dispatch.tracking-positions` têm 3 partições cada (ADR 0006).

## Decisão

`dispatch-worker`, `tracking-projector` e `notification-worker` rodam com
`replicas: 2` por padrão nos manifests do tier 3 (dentro do limite de 3
partições, com folga para uma quarta réplica em teste). Nenhum HPA foi
configurado para esses três Deployments: escalar um consumer group além do
número de partições não aumenta o throughput, só deixa réplicas ociosas.
`delivery-api` e `tracking-ingest`, que são HTTP puro sem consumer group,
têm HPA por CPU normalmente.

## Evidência

```bash
kubectl --context kind-dispatch -n dispatch scale deployment/dispatch-worker --replicas=4
docker exec dispatch-redpanda-1 rpk group describe dispatch-worker
```

**Medido** em 2026-07-26: com o `dispatch-worker` do `kind` escalado para 4
réplicas, o grupo mostrou **5 membros** (as 4 réplicas do `kind` mais uma
instância do mesmo serviço ainda rodando no docker compose, todas no mesmo
grupo `dispatch-worker` porque falam com o mesmo broker) e, mesmo assim,
só **3 aparecem na tabela de atribuição**, uma por partição. As outras duas
existem, consomem CPU e memória, mas nunca processam uma mensagem enquanto
as três atribuídas estiverem saudáveis. Evidência completa em
`docs/benchmarks/consumer-replicas-limitadas-evidencia.txt`.

## Consequências

- escalar `dispatch-worker` além de 3 réplicas é desperdício de recurso
  neste desenho. Se o throughput de consumo precisar aumentar, a alavanca
  correta é aumentar o número de partições (uma migração, ver ADR 0006), não
  o número de réplicas;
- KEDA por lag do consumer group (mencionado no roadmap como alternativa ao
  HPA de CPU) não foi configurado neste tier: com o limite de 3 partições já
  conhecido e sem evidência de lag real sob a carga testada, adicionar KEDA
  agora seria complexidade sem medição por trás.

## Status

Aceita.
