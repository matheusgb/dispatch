# Runbook: mensagem na DLQ (poison pill)

## Sintoma

Log do consumidor (`lunchrush-worker`, `tracking-projector` ou
`notification-worker`) com `"msg":"mensagem foi para a DLQ"`, incluindo
`topic`, `partition` e `offset`.

## Diagnóstico

1. Ler a mensagem na DLQ para entender por que ela não foi decodificada:

   ```bash
   docker exec lunchrush-redpanda rpk topic consume lunchrush.delivery-events.dlq -n 1
   ```

2. Causas conhecidas: JSON malformado (produtor com bug, ou mensagem de
   teste manual), evolução de schema sem compatibilidade retroativa, ou um
   evento com um `kind` que o consumidor não reconhece (isso **não** deveria
   ir para a DLQ: o handler já ignora `kind` desconhecido sem erro; se está
   na DLQ, o problema é decodificação do envelope, não o `kind`).

## Ação

- A mensagem na DLQ não bloqueou a partição original: o consumo principal
  segue normalmente (testado em `docs/benchmarks/chaos-tier-3.md`).
- Corrigir a causa raiz (schema, bug do produtor) antes de reprocessar.
- Reprocessar manualmente republicando a mensagem da DLQ de volta ao tópico
  original, depois de corrigida:

  ```bash
  docker exec lunchrush-redpanda rpk topic consume lunchrush.delivery-events.dlq -n 1 -o N \
    | jq -r .value \
    | docker exec -i lunchrush-redpanda rpk topic produce lunchrush.delivery-events
  ```

  (`N` é o offset, um inteiro puro — `rpk topic consume --help` mostra as
  formas aceitas por `-o`; confirmado rodando o comando de verdade nesta
  sessão.) Automatizado em `scripts/dlq-replay.sh`, amarrado a
  `make replay TOPIC=lunchrush.delivery-events.dlq DLQ_ID=<offset>`.

  Isso é uma ferramenta manual de replay, não automática: uma mensagem que
  foi parar na DLQ já indica uma falha que merece revisão humana antes de
  voltar ao fluxo normal.

## O que este runbook não cobre

- Reprocessamento em massa de uma DLQ com muitas mensagens: este tier não
  tem uma ferramenta dedicada para isso, só o comando manual acima mensagem
  por mensagem.
