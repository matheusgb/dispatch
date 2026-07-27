# ADR 0021: objetivo e limites da estratégia multi-cloud

## Contexto

O tier 6 (último do roadmap) pede duas clouds reais, uma matriz de
portabilidade preenchida com evidência, e um failover entre provedores que
reusa a autoridade de fencing do tier 5. Este projeto nunca usou, e nunca
vai usar, uma conta AWS real nem gastar dinheiro real (regra não-negociável
desde o tier 4, ver `docs/limitacoes-simulacao-local.md`). Isso deixa uma
pergunta em aberto que este ADR responde antes de qualquer código: dado que
"duas clouds reais" está fora de alcance, o que ainda vale a pena provar?

O próprio roadmap prevê essa saída na seção "O que não entra" do tier 6:
não promete "equivalência perfeita entre serviços gerenciados" nem "terceiro
provedor". Só que, no caso deste laboratório, nem o **segundo** provedor
pode ser uma cloud paga de verdade — é uma restrição adicional deste
projeto, mais rígida que a do roadmap original, e por isso precisa de uma
decisão explícita, não um desvio silencioso.

## Decisão

`cloud-b` é um segundo stack `docker compose` inteiramente independente do
principal (`docker-compose.cloud-b.yml`, rede Docker própria, prefixo de
projeto `cloudb`, Postgres/Redis/Redpanda/LocalStack próprios, faixa de
portas nova). `cloud-a` é o nome que este tier passa a usar para o stack
principal já existente (`docker-compose.yml`), sem renomear nada dele.

O objetivo real que essa escolha ainda ensina, dos cinco listados no
roadmap, é o único que não depende de infraestrutura gerenciada real:

> aprendizado de portabilidade — provar que o mesmo artefato (imagem OCI
> pelo mesmo digest), os mesmos contratos (schema, migrations, API, eventos)
> e o mesmo protocolo de autoridade (fencing) funcionam sem alteração
> quando apontados para um ambiente de execução diferente.

Os outros quatro objetivos do roadmap (plano de saída/redução de lock-in,
recuperação diante de indisponibilidade prolongada de um provedor real,
requisito de residência regional, comparação de custo e capacidade entre
provedores reais) **não** são o que este laboratório prova. Citá-los como
prova seria exatamente o "multi-cloud apenas nominal" que a tabela de
riscos do `lunch-rush.md` already lista como risco a evitar.

### Por que não é o mesmo experimento do tier 5

O tier 5 já prova failover **dentro** da mesma AWS simulada (células
lógicas, mesmo Postgres físico compartilhado, ver ADR 0019). O tier 6
muda uma variável que o tier 5 não mudou: duas bases de dados **fisicamente
separadas**, cada uma no seu próprio container Postgres, sem nenhum
processo compartilhado entre `cloud-a` e `cloud-b`. Isso é estritamente
mais realista que o tier 5 no que diz respeito a isolamento físico, mesmo
sem ser uma cloud real — ver ADR 0023 para o detalhe do failover.

## Evidência real

- `docker-compose.cloud-b.yml`, serviços sem `build:`, todos usando
  `image: lunchrush-<serviço>` — o mesmo nome de imagem que
  `docker compose --profile app build` já produziu para `cloud-a`;
- confirmado por `docker inspect --format '{{.Image}}'` que o container
  `lunchrush-delivery-api-1` (cloud-a) e `cloudb-delivery-api-1` (cloud-b)
  rodam exatamente o mesmo digest
  (`sha256:e3c37da8c260f47e852ffc5734cf1bdf9537a1ff6282b86b476eb096addcfa43`
  nesta execução), sem rebuild entre os dois;
- `k6 run loadtest/k6/smoke.js` contra os dois (`BASE_URL=:8083` e
  `BASE_URL=:18083`): 0% de erro nos dois, mesmo cenário, mesma seed de
  requisição — `docs/benchmarks/tier-6-portability/k6-smoke-cloud-a.json`
  e `k6-smoke-cloud-b.json`;
- `go test -tags=integration -race ./test/integration/...` passou
  integralmente contra os dois bancos (`localhost:5432` e `localhost:15432`)
  e os dois brokers Kafka (`localhost:19092` e `localhost:29093`), sem
  nenhuma alteração de código entre as duas execuções — só a variável de
  ambiente `DATABASE_URL`/`KAFKA_BROKERS` mudou.

## Alternativas consideradas

- **Um segundo provedor cloud real de baixo custo (ex.: free tier de outra
  cloud)**: rejeitada. Mesmo um free tier tem risco real de cobrança por
  erro de configuração ou esquecimento de destroy, e a regra deste projeto
  é zero risco financeiro, não "risco baixo".
- **Não implementar tier 6 algum, só documentar como não aplicável**:
  rejeitada porque o usuário pediu explicitamente para cobrir o que for
  genuinamente possível, e a maior parte do tier 6 (contratos, matriz,
  fencing cross-provedor, Terraform por provedor) não depende de uma cloud
  paga de verdade — só a comparação de custo real entre provedores depende,
  e essa parte fica documentada como limitação, não como pretexto para não
  fazer o resto.
- **Simular `cloud-b` só com Terraform, sem subir containers de aplicação
  de verdade**: rejeitada porque não provaria nada sobre o artefato (o
  ponto central do tier 6 é o mesmo binário rodando em dois lugares, não
  só "dois buckets S3 com nomes diferentes").

## Consequências

- toda alegação de "portabilidade" neste tier se refere a portabilidade de
  contrato e artefato, nunca a portabilidade de infraestrutura gerenciada
  real, nem a redução de lock-in real, nem a comparação de custo real entre
  provedores;
- `docs/limitacoes-simulacao-local.md` ganha uma seção Tier 6 expandida
  com esse recorte, para que nenhum número deste tier seja lido como prova
  de operação multi-cloud real;
- os ADRs 0022 (matriz de portabilidade) e 0023 (fencing cross-provedor)
  detalham as duas provas concretas que sustentam esta decisão.

## Status

Aceita.
