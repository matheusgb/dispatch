# ADR 0001: começar como monólito modular

## Contexto

O sistema precisa de um núcleo correto (lifecycle de entrega, atribuição de
entregador, idempotência) antes de qualquer decisão de distribuição. Nenhuma
carga medida ainda justifica separar serviços, mensageria ou orquestração.

## Decisão

O tier 1 será um único binário Go (`cmd/delivery-api`), com módulos internos
separados por responsabilidade (`internal/delivery`, `internal/courier`,
`internal/lunchrush`, `internal/platform`) e PostgreSQL como única fonte de
verdade. Nenhum desses módulos é extraído para um serviço próprio neste tier.

## Alternativas consideradas

- **Microsserviços desde o início:** rejeitado. Sem hot path medido, a
  separação criaria latência de rede, mais infraestrutura e mais superfície de
  falha sem nenhum ganho comprovado.
- **Kafka ou fila desde o início:** rejeitado. Não há requisito de
  processamento assíncrono, replay ou desacoplamento que o tier 1 precise
  resolver.

## Consequências

- a fronteira entre módulos precisa ser mantida limpa por convenção de código,
  já que o compilador não impede um módulo de importar o interno de outro
  livremente dentro do mesmo binário;
- a extração de um serviço em tier futuro só ocorre com evidência de hot path,
  necessidade de isolamento de escala ou ciclo de deploy diferente (ver tier 3
  em `lunch-rush.md`, na raiz de `labs`).

## Status

Aceita.
