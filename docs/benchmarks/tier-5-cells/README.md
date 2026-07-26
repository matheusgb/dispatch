# Arquitetura celular local: roteamento, isolamento de dados e noisy neighbor

Ver `docs/adr/0019-arquitetura-celular-local.md` para a decisão. Ambiente:
`docker compose -f docker-compose.yml -f deploy/compose/cells.yml
--profile app --profile cells up -d delivery-api-cell-a
delivery-api-cell-b cellrouter` (`cmd/cellrouter`), duas células lógicas
(`cell-a` na porta 8094, `cell-b` na porta 8095, roteador em 8096), cada
uma com seu próprio `delivery-api` e seu próprio banco
(`dispatch_cell_a`/`dispatch_cell_b`) no mesmo container PostgreSQL do
resto do laboratório.

## 1. Roteamento sem full-scan (Medido)

```text
$ curl -X POST http://localhost:8096/deliveries -H "X-Cell-ID: cell-a" ...
{"id":"76e40624-3959-4ea0-930c-a771e6e4e646","state":"created"}

$ curl -X POST http://localhost:8096/deliveries -H "X-Cell-ID: cell-b" ...
{"id":"28d82676-fb5f-44ae-b30a-63de8597e81b","state":"created"}
```

O `cellrouter` decide o backend só pelo cabeçalho `X-Cell-ID`, sem
consultar nenhum banco (`cmd/cellrouter/main.go`): é um mapa estático
`cell_id -> URL` carregado de variável de ambiente no start, exatamente o
"diretório pequeno" que o roadmap descreve (aqui sem DynamoDB Global
Tables, que está fora de alcance sem AWS real — a implementação de
referência completa fica documentada, não fingida, ver
`docs/limitacoes-simulacao-local.md`).

## 2. Isolamento de dados real (Medido, não descrito)

```sql
-- banco dispatch_cell_a
select count(*) from deliveries where id = '28d82676-...' /* criada em cell-b */;
--  0

-- banco dispatch_cell_b
select count(*) from deliveries where id = '76e40624-...' /* criada em cell-a */;
--  0
```

Cada entrega existe fisicamente em uma única tabela `deliveries`, de um
único banco, nunca nos dois: não é uma coluna `cell_id` filtrando uma
tabela compartilhada, são bancos PostgreSQL distintos (`CREATE DATABASE
dispatch_cell_a`, `dispatch_cell_b`), cada um migrado e operado pelo seu
próprio processo `delivery-api`.

## 3. Noisy neighbor (Medido)

**Hipótese:** sobrecarregar a célula A não deveria degradar materialmente
o p99 da célula B, já que cada célula tem seu próprio processo
`delivery-api` e seu próprio banco lógico.

**Baseline (cell-b sozinha, 40 requisições sequenciais, 500ms de intervalo):**

| | ms |
| --- | --- |
| média | 7,6 |
| p50 | 7 |
| p95 | 14 |
| máx | 20 |

**Durante a sobrecarga (k6, 40 VUs batendo só em cell-a por 22s, 77.568
requisições, 3.320 req/s, 31,68% de erro — cell-a saturada de propósito):**

`cell-b`, mesma sonda de 40 requisições sequenciais, rodando ao mesmo
tempo:

| | ms |
| --- | --- |
| média | 13,75 |
| p50 | 13 |
| máx | 27 |
| p95 | 24 |

**Leitura honesta:** cell-b ficou ~1,7x mais lenta durante a sobrecarga de
cell-a (p95 de 14ms para 24ms), não catastrófica, mas **não é isolamento
perfeito**. A causa mais provável é o compartilhamento físico do processo
PostgreSQL (as duas células usam bancos diferentes, mas o mesmo container,
a mesma CPU e o mesmo pool de I/O) e o host Docker compartilhado. Isso é
exatamente o que `docs/limitacoes-simulacao-local.md` já previa antes
deste tier começar: "múltiplos `docker compose` independentes... isolamento
lógico, não isolamento físico de hardware, rede ou provedor". A prova aqui
é honesta sobre o tamanho do vazamento: pequeno, mensurável, não zero.

## O que este experimento não prova

- isolamento de rede (as duas células compartilham a mesma rede Docker);
- isolamento de banco físico (mesmo processo PostgreSQL, bancos lógicos
  diferentes — um `pg_dump`/restore de uma célula nunca inclui dados da
  outra, mas as duas competem pelo mesmo `shared_buffers` e pela mesma
  CPU);
- comportamento sob falha total de uma célula (isso é o runbook de
  failover, não o teste de noisy neighbor).
