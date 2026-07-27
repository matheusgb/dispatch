# Aprendizado do tier 5

1. **O que eu não sabia no início?** Modelar fencing corretamente em TLA+ é
   mais sutil do que só "checar se o epoch bate". Fencing é a técnica de dar
   um número crescente (epoch) a cada writer que assume uma liderança, para
   que escritas de um writer antigo possam ser rejeitadas. TLA+ é uma
   linguagem de especificação formal usada para modelar sistemas
   concorrentes e verificar propriedades com o model checker TLC.

   A primeira versão da especificação usava `activeDelivery`/`activeCourier`
   como dois mapas separados (`Deliveries -> Writer`, `Couriers -> Writer`) e
   um invariante que contava, para cada entrega, quantos couriers tinham o
   mesmo writer dono. Isso não detecta dupla atribuição de verdade, detecta
   "o mesmo writer tem mais de uma coisa", o que é quase sempre verdade num
   sistema com um writer ativo. O TLC encontrou essa falha de modelagem na
   primeira execução (`Error: Invariant Safety is violated` já no segundo
   estado gerado). A correção foi trocar para `assignment: Deliveries ->
   Couriers`, uma função de verdade, onde a propriedade que importa é "um
   courier não aparece como destino de duas entregas".

2. **Que hipótese testei?** Que bastaria a guarda `owner = w` em `Assign`
   para impedir um writer antigo de escrever, sem checar o epoch
   separadamente. Estava errado, e o TLC provou isso: um writer pode se
   auto-recuperar (perder a lease e promover a si mesmo de novo, com o mesmo
   `owner_region`, mas epoch maior) enquanto ainda tem uma escrita em
   trânsito com o epoch anterior. Depois da auto-recuperação, `owner = w`
   continua verdadeiro, só o epoch mudou. Esse cenário apareceu no mutation
   test (remover só a guarda de epoch) em 4 passos. É uma forma de "writer
   antigo" mais sutil do que "duas regiões disputando": não precisa de duas
   regiões, basta uma escrita atrasada e um restart.

3. **Que evidência mudou minha opinião?** A propriedade de vivacidade
   (`EventuallyRecovers`) falhou na primeira tentativa com um contraexemplo
   real: um comportamento em que o sistema só fazia handoff de courier para
   sempre (`CourierHandoffStart` -> `CourierHandoffConfirm` ->
   `CourierHandoffActivate` -> repete), satisfazendo `WF_vars(Next)` sem
   nunca chamar `Promote`. Eu assumia que bastava declarar fairness fraca
   (garantia de que uma ação habilitada continuamente acaba acontecendo)
   sobre `Next` como um todo. O TLC mostrou que não: a fairness precisa ser
   declarada por ação (`WF_vars(Promote(w))` para cada writer), senão o
   model checker pode sempre escolher a ação mais fácil de satisfazer.

4. **Onde o sistema quebrou?** No relay do outbox, sob a carga combinada
   desta sessão (k6 de spike, TLC, SBOM/scan, tudo rodando na mesma máquina
   ao mesmo tempo): 96 a 99% dos eventos ficaram pendentes o tempo todo
   durante o drill de backup/recuperação, publicando em lotes de cerca de
   100 a cada 1min40s, em vez do ciclo de 1s esperado. Não era o que eu
   estava testando (o runbook é sobre restauração, não sobre profiling do
   relay), mas descobrir isso de graça, enquanto uma ferramenta media outra
   coisa, é o tipo de achado que este projeto pede para não esconder.

   Também quebrou o LoadGen com um pool de couriers pequeno demais: 2000
   ordens com 60 couriers e concorrência 30 produziram 1521 erros "nenhum
   entregador do pool ficou livre". Não é bug do simulador: é o pool de
   couriers saturando mais rápido do que as entregas completam, sob aquela
   combinação de parâmetros.

5. **Como diagnostiquei?** Para o relay: comparando a contagem de
   `outbox_events` pendentes antes e depois da janela de carga, e lendo os
   logs estruturados do `delivery-api` (`"eventos do outbox publicados"`)
   para ver o intervalo real entre ciclos. Para o pool de couriers: lendo a
   amostra de falhas do próprio relatório JSON do LoadGen
   (`failure_samples`), que já existia desde o tier 1 e continuou útil sem
   nenhuma mudança.

6. **Qual solução considerei e rejeitei?** Rodar 2 ou 3 stacks `docker
   compose` completos (Postgres + Redis + Kafka + 5 serviços) por célula,
   como o roadmap sugere mais diretamente. Rejeitei por causa da memória:
   mesmo depois de parar o `edge-lab`, esta máquina não sustentaria 3 stacks
   completos simultâneos junto com o resto do trabalho desta sessão (TLC,
   k6, SBOM) sem risco de travar tudo. A alternativa, bancos lógicos
   separados no mesmo Postgres físico, é mais fraca, mas é medida, não
   escondida: o teste de noisy neighbor (quando uma carga de trabalho
   prejudica outra por disputar o mesmo recurso físico) existe justamente
   para quantificar o custo dessa escolha.

7. **Que complexidade aceitei?** Um único lunchrush shard no teste de
   concorrência real (`internal/fencing`), não vários. O roadmap é explícito
   que uma única linha de fence por célula seria hot key e que múltiplos
   shards precisam de benchmark antes de escolher a quantidade. Esse
   benchmark não foi feito nesta sessão. Um shard já basta para provar a
   propriedade de segurança (writer antigo nunca escreve), mas não basta
   para provar throughput sob contenção real.

8. **O que eu faria diferente em um sistema real?** Não deixaria o relay do
   outbox compartilhar recursos com cargas de desenvolvimento/CI (TLC,
   scans de imagem) no mesmo host que serve tráfego. Parece óbvio, mas só
   ficou visível medindo, não supondo. Também dimensionaria o pool de
   couriers e a capacidade do LoadGen como parte do cenário declarado
   (igual o roadmap já exige para dataset, concorrência e duração), em vez
   de reaproveitar um valor arbitrário de uma execução menor.

9. **Qual é o próximo limite conhecido?** Multi-região real (Aurora DSQL,
   MSK Replicator, latência entre regiões AWS de verdade) continua fora de
   alcance sem uma conta AWS real. Essa é a limitação central do tier 5,
   documentada com honestidade em `docs/limitacoes-simulacao-local.md`, sem
   tratar "célula" como sinônimo de "região". O dimensionamento de múltiplos
   lunchrush shards, o failover coordenado com o LoadGen rodando ao mesmo
   tempo, e a causa raiz do atraso do relay do outbox sob carga combinada
   ficam como candidatos concretos para uma sessão futura, listados em
   `docs/benchmarks/tier-5-what-breaks-next.md`.
