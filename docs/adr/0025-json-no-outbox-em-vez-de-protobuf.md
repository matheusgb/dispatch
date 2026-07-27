# ADR 0025: manter JSON no outbox, Protobuf só documentado como referência

## Contexto

O roadmap (tier 3) pede "schemas Protobuf versionados" para os eventos de
domínio. O sistema real usa JSON desde o tier 1 (`internal/platform/outbox`,
`Envelope`) e nunca migrou. Uma auditoria encontrou essa lacuna: nem
Protobuf real, nem documentação da decisão consciente de não migrar.

## Decisão

**Não migrar o outbox/Kafka para Protobuf agora.** Continuar com JSON.
Fechar a lacuna de _entendimento_, não de _implementação_: este ADR
documenta o trade-off, e `api/proto/lunchrush/v1/delivery_events.proto`
mostra como o payload de `delivery.created` ficaria em Protobuf, como
referência para uma migração futura, sem trocar o sistema real.

## Por que não migrar agora

- **Risco desproporcional ao valor neste momento do laboratório.** O
  sistema já tem outbox, DLQ, inbox (dedup), consumer replicas e KEDA
  funcionando sobre JSON, testado em `test/integration/outbox_test.go` e
  exercitado sob carga (`docs/benchmarks/tier-4-load/`). Trocar o formato
  de serialização é uma mudança que atravessa produtor, schema, consumidor
  e todo teste de integração/contrato ao mesmo tempo — o tipo de mudança
  arriscada que sessões anteriores deste projeto (ver a lição do
  `terraform apply` que ficou pendurado contra o LocalStack) ensinaram a
  não fazer sem tempo disponível para validar de ponta a ponta.
- **JSON já resolve o problema real que o outbox tem hoje:** debug fácil
  (`rpk topic consume` mostra o payload legível, usado em todo runbook,
  ex. `docs/runbooks/dlq-replay.md`), schema evolutivo por conta própria
  (campos novos são aditivos, o consumidor antigo ignora o que não
  conhece), sem exigir um registry de schema adicional na infraestrutura.
- Protobuf exigiria decidir e operar um **schema registry** (Confluent
  Schema Registry, Buf Schema Registry, ou equivalente) para
  compatibilidade entre versões de forma segura — infraestrutura nova, sem
  contrapartida de aprendizado que o laboratório ainda não tenha coberto de
  outra forma (o laboratório já cobre versionamento de schema de forma
  mais simples via AsyncAPI documentado, `contracts/asyncapi/lunchrush-events.yaml`).

## O que fica pronto para quando migrar

`api/proto/lunchrush/v1/delivery_events.proto` define o mesmo evento
`delivery.created` (`internal/lunchrush/lunchrush.go` `enqueueDeliveryEvent`,
payload real hoje: só `delivery_id`) em Protobuf, com comentário explicando
o mapeamento campo a campo com o JSON atual. Não é consumido por nenhum
código Go de produção: é documentação executável de forma, para provar que
o schema Protobuf é trivial de escrever quando a decisão de migrar for
tomada.

**Geração de código real, não só revisão manual.** Sessão anterior deixou
isso pendente por falta de `protoc` instalado; esta sessão instalou o
binário oficial (`protocolbuffers/protobuf` v29.3, release Linux x86_64 do
GitHub, sem precisar de `apt`/sudo) e o plugin `protoc-gen-go`
(`go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`), e
rodou:

```
protoc -I api/proto -I <include dos well-known types do release> \
  --go_out=api/proto/gen --go_opt=paths=source_relative \
  api/proto/lunchrush/v1/delivery_events.proto
```

Gerou `api/proto/gen/lunchrush/v1/delivery_events.pb.go` sem erro, e
`go build ./api/...` compila o pacote gerado (`google.golang.org/protobuf`
virou dependência direta do módulo em vez de `// indirect`, confirmado por
`go mod tidy`). O `.proto` agora é validado por geração de código real, não
só por revisão manual de sintaxe. O comando ficou reproduzível como
`make proto-gen` (`Makefile`, variável `PROTOC_INCLUDE` para apontar ao
diretório `include/` do release quando `protoc` não vem de pacote de
sistema). A saída gerada não é versionada (`api/proto/gen/` no
`.gitignore`): é prova de forma reproduzível sob demanda, não artefato de
build do sistema real — nenhum pacote em `internal/` ou `cmd/` importa
`api/proto/gen`, a decisão de manter JSON no outbox real continua valendo
inalterada.

## O que mudaria se fosse produção

- **Tamanho de mensagem:** Protobuf binário é menor que JSON texto; em
  volume alto (o LoadGen já mostrou milhares de eventos por corrida),
  isso reduz custo de rede e storage no broker.
- **Validação de schema em compile-time:** um produtor Go ganharia erro de
  compilação ao esquecer um campo obrigatório, em vez de erro de runtime
  na decodificação (hoje só descoberto no consumidor, e só vira DLQ se o
  JSON for inválido — um JSON válido mas com campo faltando não é pego
  hoje, exceto se o Go struct tiver o campo como obrigatório e o
  `json.Unmarshal` falhar, o que não acontece para campo ausente em Go por
  padrão).
- **Evolução de schema controlada:** Protobuf com número de campo
  explícito e regra de compatibilidade (nunca reusar número, nunca mudar
  tipo) é mais rígido que JSON solto; isso é uma vantagem em produção com
  múltiplos times publicando/consumindo o mesmo tópico, e uma
  desvantagem de fricção quando é uma pessoa só iterando rápido, como é o
  caso deste laboratório hoje.
- Exigiria decidir entre **Protobuf com Schema Registry** (compatibilidade
  automática) ou **Protobuf sem registry** (compatibilidade garantida só
  por disciplina de code review) — produção real quase sempre quer a
  primeira opção.

## Alternativas consideradas

- **Migrar para Protobuf agora:** rejeitada pelo risco/tempo descrito acima.
- **Avro:** teria as mesmas vantagens de schema evolutivo com um registry
  mais tradicional no ecossistema Kafka; rejeitado por ser mais uma
  tecnologia nova a aprender sem ganho claro sobre Protobuf para o
  objetivo deste laboratório (estudar o trade-off, não adotar uma stack de
  streaming de dados completa).
- **Não documentar nada, deixar a lacuna:** rejeitada, é o que a auditoria
  encontrou e pediu para fechar.

## Consequências

- o sistema real continua em JSON, sem risco de regressão;
- quem quiser entender como seria a migração tem um ponto de partida real
  em `api/proto/`, não só uma frase de intenção;
- a lacuna do roadmap fica fechada como decisão documentada, não como
  "esquecido".

## Status

Aceita.
