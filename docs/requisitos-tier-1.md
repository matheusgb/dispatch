# Requisitos, não objetivos e invariantes do tier 1

## Missão

Construir um núcleo correto e mensurável em Go antes de distribuir o sistema.

## Atores

- cliente que acompanha a entrega;
- entregador que recebe oferta e envia localização;
- operação que investiga atrasos;
- sistema de pedidos que solicita uma entrega.

## Fluxo principal da entrega

```text
created
  -> ready_for_dispatch
  -> offered
  -> assigned
  -> picked_up
  -> delivered

offered -> declined -> ready_for_dispatch
offered -> expired -> ready_for_dispatch

created / ready_for_dispatch / offered / assigned -> canceled
```

## Capacidades funcionais

- criar entrega com chave de idempotência;
- cadastrar entregador e alterar disponibilidade;
- selecionar candidato por uma regra simples e determinística;
- oferecer, aceitar, recusar, expirar e concluir uma entrega;
- aplicar a máquina de estados explicitamente;
- manter trilha de auditoria das transições.

A chave de idempotência tem escopo por caller e operação, TTL documentado,
hash canônico do payload e resposta persistida. Repetir a mesma chave com o
mesmo payload devolve o resultado original. A mesma chave com payload
diferente é rejeitada como conflito.

## O que ainda não entra

- Kafka;
- Redis;
- microsserviços;
- Kubernetes;
- AWS;
- abstrações para uma escala ainda não observada.

## Invariantes exigidas a partir deste tier

1. Uma entrega possui no máximo um entregador ativo.
2. Um entregador possui no máximo uma entrega ativa.
3. Uma transição de estado só ocorre a partir de um estado permitido.
4. Um estado terminal nunca retorna a um estado anterior.
5. Repetir uma requisição com a mesma chave produz um único efeito de negócio.
6. Um comando confirmado como durável não desaparece silenciosamente.
8. Uma duplicata pode ser processada novamente, mas não pode duplicar um
   efeito interno de negócio (ledger de idempotência).

As invariantes 7, 9, 10, 11 e 12 dependem de recursos que só existem a partir
dos tiers 2, 3 e 5. Ver `dispatch.md` na raiz de `labs` para a tabela completa
de aplicabilidade por tier.

## Correção concorrente

A disputa pelo mesmo entregador é protegida por uma combinação justificável
de transação, constraint única, versionamento otimista ou update condicional,
e retry curto somente para conflitos esperados.

O teste principal lança várias tentativas simultâneas para a mesma entrega e
o mesmo entregador. O resultado esperado é exatamente uma atribuição válida.

## Critério de conclusão do tier

- 100 mil sequências geradas por fuzz sem transição inválida;
- 20 tentativas concorrentes resultam em uma atribuição;
- 10 retries simultâneos com a mesma chave e payload resultam em uma entrega;
- reuso da chave com payload diferente é rejeitado sem alterar o estado;
- nenhum data race detectado;
- histórico integral do cenário de 30 minutos sem violação observada;
- p99 e throughput publicados como medidos, sem meta disfarçada;
- pelo menos um gargalo localizado e explicado com evidência.
