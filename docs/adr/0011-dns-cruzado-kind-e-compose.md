# ADR 0011: DNS cruzado entre kind e o Redpanda do docker compose

## Contexto

O `kind` (cluster de aplicação) e o `docker compose` (infra: PostgreSQL,
Redis, Redpanda, dependency-simulator) são duas redes Docker diferentes no
mesmo host. Um `Service` do tipo `ExternalName` apontando para um IP
literal não funciona: o CoreDNS espera um nome DNS em um `CNAME`, não um
endereço, e devolve `server misbehaving` para qualquer tentativa de
resolver o nome.

Resolvido isso com `Service` + `Endpoints` manuais (ver
`deploy/kubernetes/base/external-infra.yaml`), a conexão inicial com o
Redpanda funcionou, mas toda produção e consumo real de mensagens
continuou falhando com o mesmo erro de resolução, agora para o nome
`redpanda` sozinho, sem sufixo.

## Causa

Um cliente Kafka nunca fala só com o endereço de bootstrap: depois do
primeiro contato, o broker devolve metadados dizendo qual endereço usar
para cada partição, e o cliente reconecta usando *esse* endereço. O
Redpanda do docker compose está configurado com
`--advertise-kafka-addr=PLAINTEXT://redpanda:9092`, que só resolve dentro
da rede do compose. Um pod do `kind`, ao seguir esse metadado, tentava
resolver `redpanda` sozinho e falhava.

## Decisão

Criar um segundo par `Service`/`Endpoints` no namespace `lunchrush`, com o
nome curto `redpanda` (sem sufixo `-external`), apontando para o mesmo IP
de gateway. O DNS do pod resolve nomes curtos via search domain
(`redpanda.lunchrush.svc.cluster.local`), então o metadado devolvido pelo
broker passa a resolver também.

## Alternativas consideradas

- **Mudar o `advertise-kafka-addr` do compose para o IP do host:**
  rejeitada. O IP muda por ambiente e por rede Docker; hardcodar no
  `docker-compose.yml` versionado trocaria um problema por outro.
- **`host.docker.internal` nos dois lados:** considerada, mas exigiria
  configurar `extra_hosts` no compose e um patch de CoreDNS no `kind` para
  o mesmo nome, mais complexo que replicar o Service com o nome que o
  broker já anuncia.

## Consequências

- este é um problema exclusivo de rodar infra compartilhada fora do
  cluster em duas redes Docker locais diferentes. Um MSK real ou um
  Redpanda dentro do próprio cluster não teriam essa situação: o nome
  anunciado seria alcançável por definição. Fica registrado como
  característica desta forma de simulação local, não do desenho do
  sistema (ver `docs/limitacoes-simulacao-local.md`);
- qualquer novo consumidor de Kafka adicionado a este cluster herda a
  necessidade do mesmo par de Services (`<nome>` e `<nome>-external`) até
  que a infra entre no próprio cluster (tier 4) ou vire de fato um serviço
  gerenciado (fora de escopo deste laboratório).

## Status

Aceita.
