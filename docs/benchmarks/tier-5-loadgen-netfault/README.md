# LoadGen com rede e relógio virtuais: reprodutibilidade (Medido)

Ver `docs/adr/0020-loadgen-rede-e-relogio-virtuais.md` para a decisão de
desenho. Ambiente: `docker compose --profile app` local, `delivery-api`
(8083), `tracking-ingest` (8084), `tracking-projector` (8085).

## Comando (idêntico nas duas execuções, banco truncado entre elas)

```bash
go run ./cmd/loadgen \
  --base-url http://localhost:8083 --tracking-url http://localhost:8084 --projector-url http://localhost:8085 \
  --admin-secret compose-dev-admin-secret --seed 20260726 \
  --orders 40 --couriers 15 --concurrency 8 \
  --decline-rate 0.1 --expire-rate 0.1 --duplicate-rate 0.2 \
  --net-drop-rate 0.15 --net-delay-ms 20 --net-delay-jitter-ms 30 \
  --net-duplicate-rate 0.3 --net-reorder-rate 0.3 --net-clock-skew-rate 0.3 --net-crash-rate 0.2 \
  --out run1   # e run2, com TRUNCATE entre as duas
```

## Resultado agregado (idêntico nas duas execuções)

| Métrica | run1 | run2 |
| --- | --- | --- |
| concluídas | 32 | 32 |
| recusadas | 4 | 4 |
| expiradas | 4 | 4 |
| erros | 0 | 0 |
| posições enviadas | 96 | 96 |
| posições descartadas (rede virtual) | 10 | 10 |
| posições que avançaram a projeção | 85 | 85 |
| entregadores com crash de sessão simulado | 7 | 7 |
| tentativas de clock skew | 5 | 5 |
| tentativas de clock skew seguras (sem regressão) | 5 | 5 |

## Comparação campo a campo

`run1.json` e `run2.json` comparados em Python, removendo apenas
`delivery_id` (UUID gerado pelo servidor) e `duration_ns` (tempo de
parede) de cada resultado: **os 40 resultados restantes são idênticos
byte a byte** entre as duas execuções. Script usado:

```python
def canon(path):
    d = json.load(open(path))
    for r in d["results"]:
        r.pop("delivery_id", None)
        r.pop("duration_ns", None)
    d.pop("duration_ns", None)
    return d

assert canon("run1.json") == canon("run2.json")  # True
```

## Achado real: idempotência exige truncar entre execuções comparáveis

A primeira tentativa (sem truncar o banco entre as duas execuções)
produziu 40 erros na segunda execução: a chave de idempotência
determinística devolveu a mesma entrega já criada e já avançada de estado
pela primeira execução, e `/deliveries/{id}/ready` respondeu `409`
corretamente (idempotência funcionando, não um bug). Documentado com
detalhe no ADR 0020.

## Escala desta execução

40 ordens, ~230 chamadas HTTP reais (criação, oferta, atribuição, 3
posições de GPS por entrega concluída, coleta, entrega, consulta), em
~17-19s. Isso é uma fração pequena, deliberadamente, do "milhões de
operações" que o critério de conclusão do tier 5 pede: ver
`docs/benchmarks/tier-5-baseline.md` para o volume real alcançado no soak
desta sessão e a distância honesta até a meta original do roadmap.
