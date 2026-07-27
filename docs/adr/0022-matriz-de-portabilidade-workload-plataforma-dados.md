# ADR 0022: matriz de portabilidade separando workload, plataforma e dados

## Contexto

O roadmap pede uma matriz de portabilidade por camada (artefato, execução,
API/eventos, configuração, PostgreSQL, Kafka, object storage,
observabilidade, infraestrutura, operação), com a regra explícita:
"marcar uma camada como portável exige o mesmo teste funcional ou de
contrato nos dois provedores". Este ADR decide como esse teste foi feito
neste laboratório e por que algumas linhas continuam marcadas como
específicas de provedor mesmo sendo tudo local.

## Decisão

A matriz completa vive em `docs/tier-6-matriz-portabilidade.md`. A regra
seguida para cada linha:

1. se existe um comando real, executado nos dois stacks (`cloud-a` e
   `cloud-b`), com o mesmo resultado, a linha é "portável" e o comando
   fica citado;
2. se a camada depende de algo que só existe porque os dois stacks são o
   mesmo tipo de LocalStack/Postgres/Redpanda local (não dois provedores
   gerenciados de verdade), a linha cita a evidência **e** a limitação,
   nunca só uma das duas.

Três classes de estado exigem tratamento diferente na coluna "dados"
(como o roadmap pede em "Dados, log e autoridade de escrita"):

- **PostgreSQL (lifecycle)**: portável por schema/migrations/dump lógico.
  Prova real: `pg_dump` de `cloud-a` restaurado com sucesso em `cloud-b`
  (mesmo schema, `pg_restore` sem erro de constraint), ver
  `docs/benchmarks/tier-6-portability/failover-transcript.txt`, passo 8.
- **Kafka (tracking e eventos)**: portável por protocolo (tópicos, chaves,
  schema), não testado neste tier com replicação real entre os dois
  Redpanda (cada stack tem o seu, populado independentemente pela mesma
  carga k6/LoadGen, não por replicação cross-stack). Isso é uma lacuna
  real, registrada em `docs/benchmarks/tier-6-what-breaks-next.md`.
- **Ledger de assignment (fencing)**: não é "dado que se copia e pronto":
  é a autoridade viva do protocolo. A prova aqui não é cópia de dados, é
  o protocolo de promoção em si, ver ADR 0023.

## Por que "mesmo teste, dois stacks" prova portabilidade de contrato

O ponto central: `docker-compose.cloud-b.yml` não builda nenhuma imagem
(ver ADR 0021). Se um teste de contrato passa nos dois lados usando a
mesma imagem, a única coisa que pode explicar isso é que o contrato
observável do binário (API HTTP, schema de dados que ele lê e escreve, o
jeito como consome Kafka) não depende de nenhum detalhe do ambiente em que
está rodando além das variáveis de configuração já suportadas
(`DATABASE_URL`, `KAFKA_BROKERS`, etc). Essa é exatamente a definição de
portabilidade de workload que o roadmap pede: "código de domínio não
depende de SDK específico de cloud".

## Alternativas consideradas

- **Testar só manualmente (sem k6/go test automatizados)**: rejeitada,
  contradiz "marcar uma camada como portável exige o mesmo teste funcional
  ou de contrato", que pede teste, não inspeção visual.
- **Fingir portabilidade de Kafka cross-stack sem testá-la**: rejeitada;
  a lacuna fica documentada como tal, não maquiada de "portável" só porque
  o protocolo Kafka em si (Redpanda) é o mesmo usado nos dois lados.

## Consequências

- a matriz cita comando e arquivo de evidência por linha, nunca uma
  afirmação sem link;
- a ausência de replicação Kafka real entre `cloud-a` e `cloud-b` neste
  tier é uma lacuna conhecida, não escondida, e não invalida o restante
  da matriz: PostgreSQL, API, artefato e infraestrutura (Terraform) têm
  prova de contrato completa nos dois lados.

## Status

Aceita.
