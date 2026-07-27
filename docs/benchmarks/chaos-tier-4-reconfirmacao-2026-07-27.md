# Reconfirmação real dos cenários A e D do chaos tier 4 (2026-07-27)

Os scripts reproduzíveis `chaos/local/pod-kill.sh` e
`chaos/local/postgres-latency-toxiproxy.sh` (criados numa rodada anterior
a partir dos cenários A e D de `docs/benchmarks/chaos-tier-4.md`) não
tinham sido reexecutados por falta de cluster `kind` de pé no momento.
Esta sessão subiu a infra necessária e rodou os dois scripts de verdade.
Este documento não substitui `chaos-tier-4.md` (evidência original,
mantida intacta): registra a reexecução e um bug real encontrado e
corrigido em `chaos/local/pod-kill.sh`.

## D. Latência no PostgreSQL via Toxiproxy

Rodado com `chaos/local/postgres-latency-toxiproxy.sh` de verdade (não
inline), contra um `delivery-api` local apontando para o proxy Toxiproxy
2.12.0 na frente do Postgres do `docker compose`.

- estado estável (sem toxic): média de 5ms em 10 requisições `POST /deliveries`;
- com o toxic `latency` (300ms ± 100ms jitter) ativo: média de 1841ms em
  10 requisições, **nenhuma falhou** (mesma conclusão original: mais
  lento, não incorreto);
- depois de remover o toxic: `GET /proxies` no Toxiproxy respondeu `200`
  (API de controle segue responsiva, sem sinal do deadlock do bug
  conhecido, issue Shopify/toxiproxy#558: este script só dispara
  requisições sequenciais, bem abaixo dos ~400 clientes concorrentes que
  disparam o bug);
- recuperação: média de volta a 4ms em 10 requisições, igual ao estado
  estável original.
- o script derrubou o container do Toxiproxy sozinho ao final (`trap
  cleanup EXIT`), confirmado por `docker ps` sem `chaos-toxiproxy` depois
  da execução.

Reconfirma a hipótese original sem alteração.

## A. Pod kill de uma réplica do `delivery-api`: bug real encontrado e corrigido

Primeira reexecução com `chaos/local/pod-kill.sh` reportou `100/101`
requisições e saiu com `exit 1` ("hipótese quebrou"), o que pareceria uma
regressão real. Investigação do arquivo de resultados
(`/tmp/chaos-podkill-results.txt`) mostrou a causa: a linha de log
`"matando pod $victim na requisição $i"`, escrita dentro do mesmo bloco
`( ... ) >/tmp/chaos-podkill-results.txt` que também recebe os códigos
HTTP da rajada, vazava para dentro do arquivo de resultados como uma
linha extra: 100 códigos `200` reais + 1 linha de log = 101 linhas,
`total=101` e `ok=100` no cálculo de sucesso, gerando um falso "1
requisição falhou" que nunca existiu de verdade.

Corrigido em `chaos/local/pod-kill.sh`: essa linha de log agora vai para
`stderr` (`>&2`) explicitamente, fora do redirecionamento do bloco.

Depois da correção, reexecução limpa confirmou a hipótese original: `100/100`
requisições responderam `200` durante o pod kill, `Deployment` voltou a
`2/2 Running` (`kubectl rollout status` confirmou), nenhuma requisição
perdida.

Uma segunda tentativa intermediária (antes da correção acima, mas depois
de matar um pod recém-criado de 82s de idade) ficou presa 136945ms numa
única chamada `curl` dentro do pod de debug antes de falhar com `curl:
(28)`. Não houve tempo de isolar a causa raiz com certeza (a máquina
também estava construindo 8 imagens Docker em paralelo via `act` nesse
momento, plausível causa de contenção de CPU/IO no cluster `kind`, mas
isso é hipótese, não confirmado): registrado aqui com honestidade em vez
de omitido, já que a reexecução seguinte, sem `act` rodando em paralelo,
confirmou `100/100` sem nenhuma falha.
