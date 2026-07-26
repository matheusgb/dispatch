# ADR 0003: Redis como projeção descartável, não fonte de verdade

## Contexto

A última posição de GPS precisa ser lida rápido e com frequência (o
cliente consulta ou recebe via SSE). O PostgreSQL já é a fonte de verdade
do tier 1; o tier 2 precisa decidir se o tracking cria uma segunda fonte de
verdade ou apenas um acelerador de leitura.

## Decisão

Redis guarda só a última posição conhecida por entrega, com TTL, populada
por cache-aside (miss lê do PostgreSQL e preenche) e atualizada
best-effort depois de cada escrita confirmada no PostgreSQL. Nenhuma
escrita depende do Redis estar disponível; nenhuma leitura falha se o
Redis cair, ela só fica mais lenta (cai para o PostgreSQL).

## Alternativas consideradas

- **Redis como fonte de verdade do tracking:** rejeitada. Perder o Redis
  perderia a posição atual, o que contradiz a invariante 7 (monotonicidade
  nunca pode depender de um cache volátil).
- **Sem cache, só PostgreSQL:** foi a alternativa mais simples e ainda é
  viável na carga medida neste tier (ver `tier-2-baseline.md`). Redis entra
  como experimento de aprendizado do roadmap, não porque o volume local
  exigiu.

## Consequências

- apagar o Redis inteiro é uma operação segura e sem downtime, exercitada
  no chaos local (`chaos-tier-2.md`);
- o cache nunca é invalidado por uma escrita atrasada: `RecordPosition`
  só popula o Redis quando a posição se tornou a atual no PostgreSQL,
  então uma posição fora de ordem nunca sobrescreve o cache com um valor
  velho;
- não há write-behind nem fila: a atualização do cache é uma chamada
  best-effort depois do commit, então existe uma janela (da ordem de
  milissegundos) em que o PostgreSQL já tem o dado novo e o Redis ainda
  não. Uma leitura nessa janela cai no cache com o valor antigo em vez de
  ir ao PostgreSQL, o que é aceitável para consistência eventual de
  localização, mas precisa ficar documentado: não é read-your-writes
  garantido.

## Status

Aceita.
