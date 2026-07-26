# O que quebra a seguir (tier 5)

1. **Relay do outbox sob carga combinada mantém 96-99% dos eventos
   pendentes.** Descoberto durante o drill de backup/recuperação
   (`docs/runbooks/backup-e-recuperacao-distribuida.md`): com TLC,
   scans de imagem e k6 rodando ao mesmo tempo nesta máquina
   compartilhada, o relay publicou em lotes de ~100 a cada ~1min40s, bem
   mais lento que o ciclo de ~1s esperado (README). Não investigamos
   causa raiz (contenção de CPU do laboratório vs. gargalo real do
   relay sob essa taxa de chegada) — candidato de profiling de uma
   sessão futura, com a máquina livre de outras cargas simultâneas para
   isolar a variável.

2. **O pool de couriers do LunchRush satura antes das entregas
   completarem, sob concorrência alta.** Uma tentativa de soak com 2000
   ordens, 60 couriers e concorrência 30 produziu 1521 erros "nenhum
   entregador do pool ficou livre" — não é uma dupla atribuição nem uma
   violação de invariante, é o pool ficando pequeno demais para a taxa
   de chegada de novas ordens (`runCompleted` tenta o pool inteiro antes
   de desistir). A correção é dimensionar `--couriers` proporcionalmente
   a `--concurrency` e à duração média de uma entrega completa (assign
   até deliver), não um valor arbitrário — o próprio roadmap já exige
   isso para "dataset, concorrência, duração" de cada cenário; fica
   registrado aqui como o número concreto que quebrou primeiro.

3. **Um único dispatch shard testado, não vários.** O roadmap é
   explícito: uma única linha de fence por célula seria hot key, e o
   número de shards por célula precisa vir de benchmark de contenção,
   não de suposição. `internal/fencing` só foi exercitado com um shard
   nesta sessão (suficiente para a propriedade de segurança, insuficiente
   para dimensionar capacidade). Falta: benchmark de retries OCC e
   distribuição de carga entre N shards.

4. **Failover não foi exercitado com o LunchRush gerando carga ao mesmo
   tempo.** O teste de fencing prova a propriedade de segurança sob
   concorrência controlada em teste de integração; o runbook de
   failover ainda não foi coordenado com uma execução do LunchRush
   simulando tráfego real durante a promoção — o "failover no pior
   momento possível" que o roadmap pede para o simulador.

5. **Partição control plane / data plane não modelada no LunchRush.**
   Implementada no TLA+ (implicitamente, via `knownTokens` permitindo
   tokens antigos) e coberta pelo teste de fencing (writer sem epoch
   atual = writer "particionado" da autoridade), mas não cabeada como
   uma flag de rede virtual do LunchRush em si. Toxiproxy já cobre esse
   tipo de falha nos tiers 2-4; integrá-lo ao LunchRush especificamente
   para separar o tráfego de controle (fencing) do tráfego de dados
   (GPS/lifecycle) fica como extensão futura.

6. **Multi-região real é a limitação central deste tier, não um item
   isolado.** Aurora DSQL, MSK Replicator, latência entre regiões AWS
   reais: nada disso foi testado, por não haver conta AWS real
   disponível (regra não-negociável do projeto). A implementação de
   referência do roadmap para essas peças está documentada (ADRs 0017,
   0018, 0019) e mapeada para o que foi feito localmente em
   `docs/limitacoes-simulacao-local.md`, nunca fingida.

7. **Soak de 24h/100M eventos não alcançado, por design desta sessão.**
   Ver `docs/benchmarks/tier-5-baseline.md`, seção "Soak reduzido", para
   o volume real medido e a distância honesta até a meta original do
   roadmap (que era para AWS real, não para esta máquina local).

Nenhum destes itens bloqueia a tag `tier-5.0.0`: os itens A-D do escopo
pragmático (TLA+ real, fencing multi-shard, arquitetura celular local,
simulador com rede/relógio virtuais) foram cobertos com evidência real;
os itens acima são o mapa do que continua faltando, não uma alegação de
tier completo.
