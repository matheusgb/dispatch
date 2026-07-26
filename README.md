# dispatch

Uma plataforma de última milha precisa encontrar um entregador, acompanhar o
deslocamento e manter cliente e operação informados mesmo sob disputa
concorrente, picos de tráfego e falhas parciais. Este repositório constrói
essa plataforma em Go, começando por um monólito modular correto e evoluindo,
tier por tier, até uma arquitetura celular multi-região com prova formal de
protocolo.

```text
tier atual: 1, início
REST API -> monólito modular -> PostgreSQL
```

## Invariantes já exigidas neste tier

1. Uma entrega possui no máximo um entregador ativo.
2. Um entregador possui no máximo uma entrega ativa.
3. Uma transição de estado só ocorre a partir de um estado permitido.
4. Um estado terminal nunca retorna a um estado anterior.
5. Repetir uma requisição com a mesma chave de idempotência produz um único
   efeito de negócio.
6. Um comando confirmado como durável não desaparece silenciosamente.

Ver a lista completa e a partir de qual tier cada invariante entra em
`docs/requisitos-tier-1.md`.

## Placar de resultados

| Resultado                          | Valor            | Ambiente | Duração | Relatório |
| ----------------------------------- | ---------------- | -------- | ------- | --------- |
| atribuições concorrentes corretas   | ainda não medido | n/a      | n/a     | pendente  |
| p99 de criação de entrega           | ainda não medido | n/a      | n/a     | pendente  |
| invariant violations                | ainda não medido | n/a      | n/a     | pendente  |

`ainda não medido` é mais confiável do que preencher esta tabela com metas
sem contexto. Os rótulos usados neste repositório são Premissa, Meta e Medido,
nunca um número solto.

## Como executar

Ainda não há código para executar. O próximo passo é o item 3 do backlog do
tier 1: definir a máquina de estados da entrega e os testes de propriedade
que a protegem (ver `docs/requisitos-tier-1.md`).

## Estágio atual e próximo gate

Tier 1 em andamento. O primeiro gate exige vinte tentativas concorrentes para
o mesmo entregador resultando em exatamente uma atribuição, com teste
reproduzível e explicação da decisão de banco.
