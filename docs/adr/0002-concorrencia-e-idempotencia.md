# ADR 0002: estratégia de concorrência e idempotência

## Contexto

Duas invariantes do tier 1 dependem diretamente de como o banco resolve
disputa concorrente: uma entrega tem no máximo um entregador ativo, um
entregador tem no máximo uma entrega ativa, e repetir uma requisição com a
mesma chave produz um único efeito. Nenhuma das três pode depender de lock
em memória de um processo só, porque nada nesse tier impede múltiplas
réplicas do `delivery-api` no futuro.

## Decisão

**Aceite de oferta:** um `UPDATE` condicional (`WHERE id = ? AND state =
'offered'`) decide o vencedor entre tentativas concorrentes para a mesma
entrega: a primeira transação a commitar move a linha para fora de
`offered`; qualquer tentativa posterior não encontra linha para atualizar e
recebe `ErrNotOffered`. Não há necessidade de lock explícito: a própria
linha do PostgreSQL serializa as tentativas.

**Um entregador só pode estar ativo em uma entrega:** reforçado por um
índice único parcial (`idx_deliveries_one_active_per_courier`) sobre
`deliveries.courier_id` filtrado por `state IN ('assigned', 'picked_up')`.
Duas transações tentando atribuir o mesmo entregador a entregas diferentes
resultam em uma violação de constraint para a segunda, mapeada para
`ErrCourierAlreadyActive`.

**Idempotência:** um ledger (`idempotency_keys`, chave primária composta por
`caller, operation, key`) grava o hash do payload e a resposta dentro da
mesma transação que o efeito de negócio. Uma repetição com o mesmo payload
lê o resultado gravado sem repetir o efeito; uma repetição com payload
diferente é rejeitada como conflito, sem tocar o estado.

## Alternativas consideradas

- **Lock distribuído (ex.: advisory lock do PostgreSQL, ou lock em Redis):**
  rejeitado. Adicionaria uma dependência ou uma seção crítica explícita para
  um problema que o próprio modelo relacional já resolve com constraint e
  update condicional.
- **Versionamento otimista com coluna `version` e retry no cliente:**
  considerado, mas o update condicional por estado já cumpre o mesmo papel
  aqui, porque o estado que importa (`offered`) é o próprio campo de
  versão observável do domínio. Fica reservado para casos futuros em que o
  campo de disputa não for um estado do domínio.
- **Idempotência em memória (cache local por processo):** rejeitado, porque
  não sobrevive a reinício nem funciona com mais de uma réplica.

## Consequências

- toda escrita de transição de estado precisa declarar explicitamente o
  estado esperado no `WHERE`; um `UPDATE` sem essa condição reintroduziria a
  brecha de corrida que este ADR fecha;
- o índice único parcial precisa ser mantido junto de qualquer novo estado
  que passe a contar como "ativo": hoje são `assigned` e `picked_up`;
- o ledger de idempotência cresce sem limite se nada expirar `expires_at`; o
  tier 1 ainda não tem um job de limpeza, e isso deve ser adicionado antes de
  qualquer ambiente de longa duração.

## Evidência

`docs/benchmarks/tier-1-baseline.md`: 20 tentativas concorrentes de aceite
produzem exatamente 1 atribuição, de forma reproduzível, com `-race` limpo.

## Status

Aceita.
