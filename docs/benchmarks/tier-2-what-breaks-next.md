# O que quebra a seguir (tier 2)

1. **Identidade de GPS compartilhada entre entregadores.** O LoadGen usa
   um único caller (`loadgen`) para autenticar toda a ingestão de GPS,
   então o rate limit por caller vira, na prática, um rate limit
   compartilhado entre "entregadores" simulados que deveriam ser
   independentes. Em produção, cada entregador teria seu próprio token, e
   o rate limit por caller faria sentido isolado por entregador. Isso não
   é um bug do rate limit: é uma simplificação do simulador que ficou
   visível assim que a concorrência subiu. Ver
   `loadgen-tier-2-rate-limit-compartilhado.md`.
2. **SSE não escala horizontalmente.** O broker é em memória, por
   processo (ver ADR 0004). Uma segunda réplica do `delivery-api` não
   compartilha assinantes com a primeira. Isso é aceitável com uma réplica
   só, que é o caso do tier 2, e vira bloqueador explícito no tier 3.
3. **Nenhuma medição de saturação do pool de conexões sob concorrência
   real alta.** O baseline de carga usa poucos VUs (5) ou concorrência
   moderada do LoadGen (até 15). Falta um cenário de stress ou
   breakpoint que empurre o pool do PostgreSQL e do Redis até o limite.
4. **Token sem revogação.** Um JWT válido continua válido até expirar (1h),
   mesmo que o segredo administrativo mude de dono ou o caller devesse
   perder acesso antes disso. Aceitável no tier 2 (ver ADR 0005), não em
   produção real.
5. **Notificação sem retry nem outbox.** `internal/notification` é
   fire-and-forget: se o `dependency-simulator` estiver fora do ar no
   momento exato da chamada, a notificação simplesmente não é reenviada.
   O roadmap só resolve isso no tier 3 com outbox e at-least-once.
6. **Loki e Tempo não entraram neste tier.** O dashboard cobre métricas
   (Prometheus/Grafana); logs estruturados já existem (`slog` em JSON),
   mas não há agregador de logs nem tracing distribuído. Foi uma escolha
   de escopo, não uma limitação técnica: dado o tamanho já grande do tier
   2, ficou para quando houver mais de um serviço para correlacionar
   (tier 3), que é quando tracing realmente compensa o custo de operar.

Nenhum destes itens bloqueia a tag `tier-2.0.0`: o critério de conclusão do
tier 2 pede um produto local operável e observável com fallback
comprovado, não ausência de todo limite conhecido.
