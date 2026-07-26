# Aprendizado do tier 5

1. **O que eu não sabia no início?** Que modelar fencing em TLA+
   corretamente é mais sutil do que "checar se o epoch bate". A primeira
   versão da especificação usava `activeDelivery`/`activeCourier` como
   dois mapas separados (`Deliveries -> Writer`, `Couriers -> Writer`) e
   um invariante que contava, para cada entrega, quantos couriers tinham
   o mesmo writer dono — isso não detecta dupla atribuição de verdade,
   detecta "o mesmo writer tem mais de uma coisa", o que é praticamente
   sempre verdade num sistema com um writer ativo. O TLC encontrou essa
   falha de modelagem na primeira execução (`Error: Invariant Safety is
   violated` já no segundo estado gerado). A correção foi trocar para
   `assignment: Deliveries -> Couriers`, uma função de verdade, onde a
   propriedade "um courier não aparece como destino de duas entregas" é o
   que realmente importa.

2. **Que hipótese testei?** Que bastaria a guarda `owner = w` em `Assign`
   para impedir um writer antigo de escrever, sem checar o epoch
   separadamente. Errado, e o TLC provou: um writer pode se
   **auto-recuperar** (perder a lease, promover a si mesmo de novo, mesmo
   `owner_region`, epoch maior) enquanto ainda tem uma escrita em trânsito
   com o epoch anterior. `owner = w` continua verdadeiro depois da
   auto-recuperação; só o epoch mudou. Esse foi o cenário que o mutation
   test (remover só a guarda de epoch) revelou em 4 passos, e é uma forma
   de "writer antigo" mais sutil do que "duas regiões diferentes
   disputando": nem precisa de duas regiões, precisa só de uma escrita
   atrasada e um restart.

3. **Que evidência mudou minha opinião?** A propriedade de vivacidade
   (`EventuallyRecovers`) falhou na primeira tentativa com um
   contraexemplo genuíno: um comportamento onde o sistema só fazia
   handoff de courier para sempre (`CourierHandoffStart` ->
   `CourierHandoffConfirm` -> `CourierHandoffActivate` -> repete),
   satisfazendo `WF_vars(Next)` sem nunca chamar `Promote`. Eu assumia que
   fairness fraca sobre `Next` como um todo bastaria; o TLC mostrou que
   não — fairness precisa ser declarada por ação (`WF_vars(Promote(w))`
   para cada writer), senão o model checker é livre para escolher sempre
   a ação "mais fácil" de satisfazer.

4. **Onde o sistema quebrou?** No relay do outbox, sob a carga combinada
   desta sessão (k6 de spike, TLC, SBOM/scan, tudo rodando na mesma
   máquina ao mesmo tempo): 96-99% dos eventos ficaram pendentes o tempo
   todo durante o drill de backup/recuperação, publicando em lotes de
   ~100 a cada ~1min40s em vez do ciclo de ~1s esperado. Isso não era o
   que eu estava testando (o runbook é sobre restauração, não sobre
   profiling do relay), mas descobrir isso "de graça" enquanto uma
   ferramenta media outra coisa é exatamente o tipo de achado que este
   projeto pede para não esconder. Também quebrou o LunchRush com courier
   pool pequeno demais: 2000 ordens com 60 couriers e concorrência 30
   produziram 1521 erros "nenhum entregador do pool ficou livre" — não é
   bug do simulador, é o pool de couriers saturando mais rápido do que as
   entregas completam sob aquela combinação de parâmetros.

5. **Como diagnostiquei?** Para o relay: comparando a contagem de
   `outbox_events` pendentes antes e depois da janela de carga, e lendo os
   logs estruturados do `delivery-api` (`"eventos do outbox publicados"`)
   para ver o intervalo real entre ciclos. Para o pool de couriers: lendo
   a amostra de falhas do próprio relatório JSON do LunchRush
   (`failure_samples`), que já existia desde o tier 1 e continuou útil sem
   mudança nenhuma.

6. **Qual solução considerei e rejeitei?** Rodar 2-3 stacks `docker
   compose` completos (Postgres+Redis+Kafka+5 serviços) por célula, como
   o roadmap sugere mais diretamente. Rejeitei por memória: mesmo depois
   de parar o `edge-lab`, esta máquina não sustentaria 3 stacks completos
   simultâneos com o resto do trabalho desta sessão (TLC, k6, SBOM) sem
   arriscar travar tudo. A alternativa (bancos lógicos separados no mesmo
   Postgres físico) é honestamente mais fraca, mas é medida, não
   escondida: o teste de noisy neighbor existe justamente para quantificar
   o quanto essa escolha custa.

7. **Que complexidade aceitei?** Um único dispatch shard no teste de
   concorrência real (`internal/fencing`), não vários. O roadmap é
   explícito que uma única linha de fence por célula seria hot key e que
   múltiplos shards precisam de benchmark antes de escolher a quantidade
   — não fiz esse benchmark nesta sessão. Um shard já basta para provar a
   propriedade de segurança (writer antigo nunca escreve), não basta para
   provar throughput sob contenção real.

8. **O que eu faria diferente em um sistema real?** Não deixaria o relay
   do outbox compartilhar recursos com cargas de trabalho de
   desenvolvimento/CI (TLC, scans de imagem) no mesmo host que serve
   tráfego — pareceria óbvio, mas só ficou visível medindo, não
   supondo. Também dimensionaria o pool de couriers/capacidade do
   LunchRush como parte do cenário declarado (igual ao roadmap já exige
   para dataset/concorrência/duração), não como um valor arbitrário
   reaproveitado de uma execução menor.

9. **Qual é o próximo limite conhecido?** Multi-região real (Aurora DSQL,
   MSK Replicator, latência entre regiões AWS de verdade) continua fora
   de alcance sem conta AWS real — essa é a limitação central do tier 5,
   documentada com honestidade em `docs/limitacoes-simulacao-local.md`,
   não escondida atrás de "célula" como sinônimo de "região". O
   dimensionamento de múltiplos dispatch shards, o failover coordenado
   com o LunchRush rodando ao mesmo tempo, e a causa raiz do atraso do
   relay do outbox sob carga combinada ficam como candidatos concretos de
   uma sessão futura, listados em
   `docs/benchmarks/tier-5-what-breaks-next.md`.
