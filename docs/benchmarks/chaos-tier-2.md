# Chaos local do tier 2

Cada experimento segue: hipótese, injeção, observação, recuperação.

## 1. Remover o Redis completamente

**Hipótese:** se o Redis ficar indisponível, consultas de tracking têm
latência maior (fallback para PostgreSQL), mas a fonte de verdade continua
íntegra e nenhuma leitura falha.

**Injeção:** `docker compose stop redis` com o `delivery-api` no ar.

**Observação:** `GET /deliveries/{id}/position` continuou respondendo `200`
com o valor correto, lendo direto do PostgreSQL. Nenhuma requisição falhou.

**Recuperação:** `docker compose start redis`. O cache volta a ser
populado na primeira leitura seguinte (miss -> populate), sem intervenção
manual.

**Confirma:** o ADR "Redis como projeção, não fonte de verdade" (ver
`docs/adr/0003-redis-como-projecao.md`).

## 2. Matar o delivery-api no meio da carga

**Hipótese:** interromper o processo durante requisições em andamento não
deixa nenhum entregador com duas entregas ativas nem corrompe uma
transação parcial: o PostgreSQL só confirma o que comitou.

**Injeção:** `docker kill dispatch-delivery-api-1` com o LunchRush
disparando 80 ordens em concorrência 8 (seed 9001), religado ~2s depois com
`docker compose up -d delivery-api`.

**Observação:** 68 das 80 ordens falharam (conexão recusada durante a
janela de indisponibilidade), 7 concluíram e 5 foram recusadas antes da
queda. Depois da religação, `SELECT courier_id, count(*) FROM deliveries
WHERE state IN ('assigned','picked_up') GROUP BY courier_id HAVING
count(*) > 1` devolveu zero linhas: nenhum entregador ficou com duas
entregas ativas. Evidência completa em
`chaos-tier-2-kill-delivery-api.md`.

**Recuperação:** automática, sem passo manual: o próximo request encontra o
processo novo e o PostgreSQL com o estado exato de antes da queda.

## 3. Latência no PostgreSQL via Toxiproxy

**Hipótese:** latência alta no banco degrada o p95 das requisições, mas não
produz erro nem corrompe estado: o sistema fica mais lento, não incorreto.

**Injeção:** Toxiproxy 2.12.0 em frente ao PostgreSQL local, toxic
`latency` de 300ms ± 100ms de jitter, com o k6 smoke rodando durante a
injeção.

**Observação:**

| Cenário | p95 | Requisições com sucesso |
| --- | --- | --- |
| baseline | 4,5 ms | 100% |
| com latência injetada | 1,5 s | 100% |
| depois de remover o toxic | 3,75 ms | 100% |

Nenhuma requisição falhou em nenhum dos três momentos: o threshold de
`p(95)<500ms` do k6 quebrou durante a injeção, como esperado, mas
`http_req_failed` ficou em 0% do início ao fim.

**Cuidado conhecido:** o Toxiproxy 2.12.0 tem um bug de deadlock na API de
controle ao remover toxics sob alta concorrência com `reset_peer`
([issue #558](https://github.com/Shopify/toxiproxy/issues/558), reproduzido
em `toxiproxy-repro/` na raiz de `labs`, fora deste repositório). Este
experimento usou 5 VUs e um único toxic de latência, bem abaixo da
concorrência que dispara o bug (400 clientes no repro). A API de controle
foi conferida como responsiva depois da remoção do toxic.

## O que não foi testado neste tier

- reinício sequencial de múltiplos containers (só há uma réplica do
  `delivery-api` no tier 2, então não há "sequencial" a testar ainda);
- saturação do pool de conexões do PostgreSQL sob concorrência real alta
  (falta o cenário de carga soak, ver `tier-2-what-breaks-next.md`);
- interrupção do stream SSE com verificação de fallback para polling: o
  mecanismo existe (`GET /deliveries/{id}/position`), mas não foi
  exercitado sob desconexão forçada do cliente neste tier.
