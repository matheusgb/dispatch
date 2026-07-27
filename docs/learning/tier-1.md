# Aprendizado do tier 1

1. **O que eu não sabia no início?** Se um índice único parcial do
   PostgreSQL (`UNIQUE ... WHERE state IN (...)`) seria suficiente para
   impedir um entregador com duas entregas ativas, sem precisar de lock
   explícito ou verificação prévia em código.

2. **Que hipótese testei?** Que um `UPDATE` condicional por estado, mais uma
   constraint única, resolvem sozinhos a disputa de "20 tentativas
   concorrentes, exatamente uma vencedora", sem coordenação em memória da
   aplicação.

3. **Que evidência mudou minha opinião?** Nenhuma. A hipótese se confirmou de
   primeira: `-race` limpo e o teste de 20 goroutines determinístico em
   execuções repetidas. O ajuste foi no teste, não na implementação. O
   primeiro teste de "duas entregas disputam o mesmo entregador" usava a
   mesma chave de idempotência para as duas entregas de setup, então as duas
   chamadas devolviam a mesma entrega em vez de duas entregas distintas. O
   bug estava no cenário de teste, não na lógica de atribuição.

4. **Onde o sistema quebrou?** Em nenhum teste de correção. O ponto fraco foi
   de desempenho: o perfil de CPU mostrou que os caminhos de criação e de
   atribuição passam a maior parte do tempo bloqueados em I/O de rede com o
   PostgreSQL, não em CPU da aplicação.

5. **Como diagnostiquei?** `go tool pprof -top` sobre o perfil de CPU
   coletado durante os benchmarks contra o PostgreSQL local: 10,89% de
   utilização de CPU ao longo da duração total, com `runtime.futex` e
   `Syscall6` dominando as amostras.

6. **Qual solução considerei e rejeitei?** Um lock distribuído (advisory
   lock do PostgreSQL ou um lock em Redis) para a disputa de atribuição.
   Rejeitei porque o próprio modelo relacional, com update condicional e
   constraint única, já resolve o problema sem dependência nova e sem seção
   crítica explícita para manter.

7. **Que complexidade aceitei?** Um loop de expiração de ofertas por
   polling fixo a cada 5 segundos, sem índice dedicado em
   `offer_expires_at`. Isso não escala indefinidamente. Documentei o limite
   em `docs/benchmarks/tier-1-what-breaks-next.md` em vez de otimizar
   antecipadamente algo sem carga real medida.

8. **O que eu faria diferente em um sistema real?** Definiria `MaxConns` no
   pool de conexões desde o primeiro deploy, mesmo sem evidência de
   saturação. É uma linha de configuração, não uma otimização prematura. Sem
   ela, o limite acaba sendo descoberto pela pior via possível: em produção.

9. **Qual é o próximo limite conhecido?** Ainda não existe medição de
   throughput sob concorrência real: os números do baseline são de um
   processo sequencial, um benchmark, uma máquina. O próximo passo é o
   LoadGen e os cenários de carga em k6 do backlog do tier 1, antes de
   qualquer decisão sobre separar serviços no tier 3.
