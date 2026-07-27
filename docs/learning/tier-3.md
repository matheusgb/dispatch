# Aprendizado do tier 3

1. **O que eu não sabia no início?** Que um `Service` do Kubernetes do tipo
   `ExternalName` não aceita um IP literal. Ele precisa de um nome DNS de
   verdade, senão o CoreDNS devolve `server misbehaving` para qualquer
   consulta. O padrão certo para apontar para fora do cluster usando um IP é
   `Service` sem seletor combinado com `Endpoints` manual.

2. **Que hipótese testei?** O padrão outbox
   grava o evento na mesma transação do efeito de negócio e publica depois.
   Testei se ele sobrevive a um relay que morre entre o ack do Kafka e a
   marca de publicado, sem perder nem duplicar o efeito, graças ao inbox do
   lado do consumidor.

3. **Que evidência mudou minha opinião?** Nenhuma mudou a decisão. Mas o
   teste que simula esse crash (`TestOutbox_CrashBeforeMarkRepublishes...`)
   só passou depois que eu percebi que o tópico acumula mensagens de
   execuções de teste anteriores. Um consumer group novo lê desde o offset
   mais antigo, então sem filtrar por `event_id` o teste contava mensagens de
   outra execução como se fossem a duplicata esperada.

4. **Onde o sistema quebrou?** Duas vezes, as duas em rede, nenhuma em
   lógica de domínio:
   - o Redpanda do docker compose anuncia a si mesmo como `redpanda:9092`,
     endereço que só existe dentro da rede do compose. Um cliente Kafka
     rodando no `kind` conseguia o primeiro contato, mas falhava toda
     produção e consumo real ao seguir o metadado devolvido pelo broker;
   - `runAsNonRoot: true` sem `runAsUser` explícito falha contra uma imagem
     distroless cujo `USER` é o nome simbólico `nonroot`, porque o kubelet
     não consegue verificar que um nome não numérico é de fato não root.

5. **Como diagnostiquei?** Direto com `kubectl logs` e `kubectl describe
   pod`. O primeiro erro (`CreateContainerConfigError`) apontou exatamente
   para o `runAsNonRoot`. O segundo (`lookup redpanda on ...: server
   misbehaving`) só apareceu nos logs da aplicação, não em eventos do
   Kubernetes, porque do ponto de vista do cluster o pod estava saudável.

6. **Qual solução considerei e rejeitei?** Hardcodar o IP do host no
   `docker-compose.yml` como `advertise-kafka-addr`. Rejeitei porque esse IP
   muda por máquina e por rede Docker. Um arquivo versionado com um valor
   específico deste ambiente seria falso para qualquer outra pessoa que
   clonasse o repositório.

7. **Que complexidade aceitei?** Dois pares `Service`/`Endpoints` para o
   mesmo IP de infra externa (`redpanda` e `redpanda-external`), só porque o
   protocolo Kafka exige que o nome anunciado pelo broker resolva de
   verdade. É uma muleta específica de rodar `kind` e `docker compose` como
   duas redes Docker separadas (ADR 0011), não uma decisão de arquitetura.

8. **O que eu faria diferente em um sistema real?** Colocaria o Kafka
   (gerenciado ou não) na mesma rede ou VPC dos consumidores desde o início,
   em vez de misturar dois ambientes de container locais. O problema que
   consumiu mais tempo de diagnóstico neste tier não existiria com MSK ou
   com Redpanda dentro do próprio cluster.

9. **Qual é o próximo limite conhecido?** Redpanda de nó único, sem réplica
   nem ISR. Qualquer alegação de durabilidade além de "o processo não
   morreu" precisa esperar o tier 4, quando um cluster Kafka multi-broker
   (real ou simulado localmente) entrar em cena.
