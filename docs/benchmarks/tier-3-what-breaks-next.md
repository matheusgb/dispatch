# O que quebra a seguir (tier 3)

1. **Latência composta do relay do outbox.** `created -> offered` leva
   ~3,8s isolado e passa de 30s sob concorrência 8 (ADR 0009). Se um
   cenário de produto precisar de resposta mais rápida, a alavanca é
   LISTEN/NOTIFY do PostgreSQL para acordar o relay sem esperar o próximo
   tick, não simplesmente reduzir o intervalo (que aumentaria a carga de
   polling proporcionalmente).
2. **`realtime-gateway` continua dentro do `tracking-projector`.** ADR 0008
   documenta que isso é uma escolha por falta de evidência de contenção,
   não uma limitação técnica. O gatilho para separar é um teste de carga
   que mostre conexões SSE competindo por CPU com o consumo de Kafka no
   mesmo processo.
3. **Redpanda de nó único.** Sem réplica, sem ISR, sem tolerância a falha
   de broker. O tier 4 (MSK real ou uma simulação equivalente) é quando
   isso passa a importar; medir antes disso seria simular uma garantia que
   este ambiente não tem.
4. **DNS cruzado entre `kind` e a infra externa é uma muleta.** Funciona
   (ADR 0011), mas qualquer novo consumidor precisa lembrar de criar o
   Service com o nome curto que o Redpanda anuncia. Isso é específico da
   forma de simular localmente (duas redes Docker), não do desenho do
   sistema: documentado em `docs/limitacoes-simulacao-local.md`, não seria
   um problema com infra gerenciada de verdade ou com o Redpanda dentro do
   próprio cluster.
5. **Nenhum teste de evolução de schema.** O roadmap pede "teste de replay
   atravessará ao menos uma evolução real de schema": os envelopes usam
   JSON simples (`internal/platform/outbox.Envelope`), sem Protobuf nem
   verificação de compatibilidade. Ficou fora deste tier por escopo: JSON
   sem schema registry não tem como testar compatibilidade formal, e
   introduzir Protobuf só para isso seria complexidade sem a pergunta que
   justificaria (nenhum consumidor deste laboratório mudou de versão
   ainda).
6. **HPA de CPU para `delivery-api` e `tracking-ingest`, nenhum para os
   consumidores.** Correto pelo ADR 0010 (réplica além de partição não
   ajuda), mas significa que o único jeito de ver o HPA de fato escalar é
   gerar carga HTTP real, sustentada, o que não foi medido neste tier
   (o teste de HPA ficou limitado a confirmar que o `metrics-server`
   reporta CPU corretamente, não a um evento de scale-up observado).

Nenhum destes itens bloqueia a tag `tier-3.0.0`: o critério de conclusão
do tier 3 pede um sistema distribuído correto sob duplicata e fora de
ordem, não ausência de todo limite conhecido.
