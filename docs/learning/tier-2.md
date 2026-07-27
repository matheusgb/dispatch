# Aprendizado do tier 2

1. **O que eu não sabia no início?** Empilhar `http.ResponseWriter` em
   camadas de middleware (log, métricas) esconde `http.Flusher` do handler
   de SSE. Desde Go 1.20, a correção certa é implementar `Unwrap()` nos
   wrappers, para que `http.ResponseController` atravesse as camadas. Não é
   preciso colocar o SSE como primeiro middleware da cadeia.

2. **Que hipótese testei?** Que um `UPSERT` condicional
   (`ON CONFLICT ... WHERE estado_atual < estado_novo`) no PostgreSQL basta
   para garantir a monotonicidade de posição por `(epoch, sequence)`. Sem
   lock explícito, sem comparação em memória antes de escrever.

3. **Que evidência mudou minha opinião?** Nenhuma mudou a decisão de
   arquitetura. Mas o teste equivalente ao
   `TestLunchRush_CourierCannotHoldTwoActiveDeliveries` para GPS
   (`TestTracking_NewEpochSupersedesOldSequence`) confirmou um detalhe
   sutil: um novo epoch com sequence baixa precisa vencer um epoch antigo
   com sequence alta. A comparação de tupla
   `(epoch, sequence) < (epoch, sequence)` no SQL já resolve isso, porque
   compara epoch primeiro. Mas isso só ficou confirmado depois de escrever
   o teste que força esse caso.

4. **Onde o sistema quebrou?** No próprio LoadGen, não no delivery-api. Ao
   reusar uma identidade só para todas as ordens simuladas, a concorrência
   de teste virou concorrência de rate limit dentro da mesma identidade. O
   rate limit funcionou como desenhado. O problema foi o simulador não
   modelar entregadores como identidades independentes.

5. **Como diagnostiquei?** Lendo a amostra de falhas do relatório do
   LoadGen (`status 429`) e comparando com o rate limit configurado
   (20 rps, burst 40) contra o número de chamadas de GPS por segundo que a
   concorrência escolhida gerava.

6. **Qual solução considerei e rejeitei?** Aumentar o rate limit global só
   para o LoadGen passar sem erro. Rejeitei porque isso escondia o achado
   real (identidade compartilhada) atrás de um número maior, em vez de
   documentar a causa. Documentar o achado
   (`tier-2-what-breaks-next.md`) preserva a informação para quando o
   tier 3 introduzir identidade por entregador de verdade.

7. **Que complexidade aceitei?** Um broker de SSE em memória, que não
   escala além de uma réplica (ADR 0004). Isso está documentado como
   bloqueador explícito do tier 3, não escondido como se já resolvesse o
   caso geral.

8. **O que eu faria diferente em um sistema real?** Geraria um token por
   entregador desde o início do simulador de carga, não só um token por
   "serviço simulado". O custo de fazer isso direito no LoadGen é baixo e o
   ganho de realismo é alto: a maior parte dos bugs de rate limit em
   produção vem de identidade mal modelada, não do algoritmo do limitador.

9. **Qual é o próximo limite conhecido?** Nenhuma medição de stress ou
   breakpoint ainda. Os números atuais vêm de smoke tests e do LoadGen em
   concorrência moderada. O tier 3 precisa decidir se a extração de
   `tracking-ingest` e `realtime-gateway` é justificada por uma medição
   real de saturação, não pelo diagrama do roadmap.
