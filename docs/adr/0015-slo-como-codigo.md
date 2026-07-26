# ADR 0015: SLOs como código em Prometheus

## Contexto

O tier 4 pede "SLOs como código": objetivos de nível de serviço
declarados em texto versionado, não em uma planilha ou num painel
configurado manualmente, com alertas de queima de orçamento de erro
(burn-rate), não só um alerta de limiar simples.

## Decisão

Três SLOs, cada um com regras de gravação (`record`) e de alerta
(`alert`) em `observability/prometheus/rules/`, carregadas por
`prometheus.yml` via `rule_files: ["rules/*.yml"]`:

1. **Disponibilidade do `delivery-api`** (`slo-delivery-api.yml`): Meta
   99,5% de requisições não-5xx em 30 dias.
2. **Latência do `delivery-api`** (`slo-delivery-api-latency.yml`): Meta
   95% das requisições em até 500ms em 30 dias (mesmo limiar já usado
   como threshold de aceite no k6 smoke deste repositório).
3. **Disponibilidade do `tracking-ingest`** (`slo-tracking-ingest.yml`):
   Meta 99% de requisições não-5xx em 30 dias, deliberadamente mais
   frouxa que a do `delivery-api` porque `tracking-ingest` não usa outbox
   (ADR 0007): perder uma posição de GPS intermediária é aceitável,
   perder uma transição de entrega não.

Todos os números de Meta acima são premissas razoáveis para um sistema
deste porte, não medições: este laboratório não acumulou 30 dias de
tráfego real (ver `docs/limitacoes-simulacao-local.md`).

## Técnica: burn-rate multi-janela

Cada SLO gera quatro `record` (razão de erro/violação de latência nas
janelas 5m, 30m, 1h, 6h) e dois `alert` (queima rápida: 14.4x o
orçamento sustentado simultaneamente em 5m e 1h; queima lenta: 6x
sustentado em 30m e 6h). Os multiplicadores 14.4 e 6 vêm da tabela padrão
do Google SRE Workbook
(<https://sre.google/workbook/alerting-on-slos/>) para um período de 30
dias com dois alertas; não foram recalculados aqui, citados como
premissa herdada da fonte. A vantagem sobre um alerta de limiar único: a
janela curta detecta queima rápida (incidente agudo) sem esperar a janela
longa acumular, e a janela longa evita alertar por um pico de 2 minutos
que não teria impacto real no orçamento de 30 dias — as duas precisam
concordar para o alerta disparar.

## Bug encontrado na validação: divisão por vetor vazio no PromQL

A primeira versão da razão de erro (`errors / total`) ficou sem
resultado (`vector()`) sempre que não havia nenhuma requisição com
`status=~"5.."` na janela: o numerador era um vetor instantâneo vazio (a
série com esse label nunca existiu, porque nunca houve erro), e uma
divisão `vetor_vazio / vetor_com_dados` no PromQL produz um resultado
vazio (o operador binário exige encontrar um par correspondente dos dois
lados), não `0` como seria intuitivo. `curl .../api/v1/query` confirmou
isso na prática: a série `slo:delivery_api_errors:ratio_rate5m`
simplesmente não aparecia depois de tráfego 100% bem-sucedido.

Corrigido envolvendo o numerador com `... or on() vector(0)`: se o lado
esquerdo (`sum(rate(...status=~"5.."...))`) não existir, o `or`
substitui por um vetor escalar `0`, e a divisão sempre produz um
resultado. Depois da correção, `slo:delivery_api_errors:ratio_rate5m`
passou a reportar `0` de verdade com tráfego 100% bem-sucedido, em vez de
ficar ausente (o que, numa consulta de alerta, teria o efeito perigoso de
nunca disparar `absent()`-style mas também nunca aparecer em um
dashboard, mascarando silenciosamente o SLO).

## Evidência

Depois de reiniciar o Prometheus com as regras montadas
(`docker compose --profile observability up -d prometheus`) e gerar 20
requisições `POST /deliveries` bem-sucedidas:

```text
$ curl .../api/v1/rules
6 grupos carregados (3 de gravação, 3 de alerta), 0 com erro de avaliação

$ curl '.../api/v1/query?query=slo:delivery_api_errors:ratio_rate5m'
value: 0

$ curl '.../api/v1/query?query=slo:delivery_api_latency_breach:ratio_rate5m'
value: 0

$ curl .../api/v1/alerts
0 alertas ativos (esperado: sem erro nem latência acima da Meta)
```

Evidência completa em `docs/benchmarks/tier-4-slo-evidencia.txt`. O
cenário de chaos D (`docs/benchmarks/chaos-tier-4.md`, latência via
Toxiproxy no PostgreSQL) já produziu exatamente o tipo de degradação que
`DeliveryAPILatencyBudgetBurnFast` foi desenhado para capturar
(referenciado na anotação do próprio alerta); não foi re-executado nesta
passada só para acionar o alerta porque o efeito (latência multiplicada
por round trips) já está medido e citado.

## Alternativas consideradas

- **Alertmanager para roteamento de notificação:** fora de escopo desta
  decisão; este ADR cobre a definição dos SLOs como código e sua
  avaliação pelo Prometheus, não o roteamento/entrega da notificação
  (nenhum canal real de paging existe neste laboratório).
- **SLO único de "tudo ou nada" por serviço:** rejeitado; disponibilidade
  e latência são violações de natureza diferente (erro vs. lentidão) e
  merecem orçamentos e alertas separados, como o próprio cenário de chaos
  D mostrou (latência alta sem nenhum erro).

## Consequências

- qualquer novo endpoint crítico adicionado ao sistema pode reaproveitar
  o mesmo padrão de arquivo (`record` de razão por janela + `alert` de
  burn-rate de duas velocidades);
- o `or on() vector(0)` é necessário em toda razão de erro/violação
  construída sobre um `status=~"5.."` ou equivalente que pode legitimamente
  não ter série nenhuma; qualquer SLO novo neste repositório deve seguir
  o mesmo padrão para não repetir o bug.

## Status

Aceita.
