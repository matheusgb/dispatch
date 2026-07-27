# Threat model (STRIDE) por ameaça do roadmap

Este documento cobre as ameaças listadas na seção "Ameaças que precisam de
teste" do roadmap (fora deste repositório), verificadas contra o código
real em `internal/` e `cmd/`, não contra intenção de design. Cada ameaça
diz explicitamente o que está mitigado hoje e o que não está. Números e
citações de arquivo:linha foram conferidos na leitura do código nesta
sessão; se o código mudar, este documento pode ficar desatualizado e
precisa ser revisado junto com qualquer PR que toque autenticação,
autorização ou tracking.

Categoria STRIDE entre parênteses em cada ameaça (Spoofing, Tampering,
Repudiation, Information disclosure, Denial of service, Elevation of
privilege).

## 1. Usuário consulta tracking de entrega alheia (Information disclosure)

**Mitigado.** `cmd/tracking-projector/http.go` tem `authorizeOwner`
(linhas 40-55): busca o dono da entrega via
`internal/delivery/postgres.go` `Owner()` (linhas 105-118, `SELECT
created_by_caller FROM deliveries WHERE id = $1`) e compara com
`auth.Caller(r.Context())`, extraído do JWT validado por
`internal/platform/auth/auth.go` `Middleware`. Se o dono for diferente ou
vazio (entrega sem `created_by_caller`), devolve 403. Os três endpoints de
leitura de tracking (`GET /deliveries/{id}/position`,
`GET /deliveries/{id}/positions`, `GET /deliveries/{id}/stream`) passam por
essa checagem antes de responder.

Detalhe de design: se `created_by_caller` for NULL no banco, `Owner()`
devolve string vazia e `authorizeOwner` nega para todo mundo (fail-closed,
não fail-open) — comportamento seguro, mas que pode parecer bug de acesso
para quem não sabe disso.

**Não testado antes desta sessão.** Não havia teste automatizado que
exercitasse `authorizeOwner` esperando 403. Fechado nesta sessão: ver
`internal/delivery/authorization_test.go` (teste negativo real, seção
"Testes negativos adicionados" abaixo).

**Lacuna real:** o `delivery-api` (`internal/platform/httpapi/server.go`)
não tem equivalente. `GET /deliveries/{id}` (handler `handleGetDelivery`,
linhas 115-127) não passa por nenhum middleware de autenticação e devolve
o estado completo da entrega (incluindo `courier_id`) para qualquer
requisição, autenticada ou não. Isso é uma divergência real de postura
entre os dois serviços: tracking exige dono, lifecycle não exige nada.
Registrado aqui como pendência conhecida, não coberto neste passe (mudar
isso afeta o fluxo de dispatch-worker e testes de integração existentes, é
mudança de comportamento, não fechamento de lacuna de documentação).

## 2. Entregador envia posição para entrega que não lhe pertence (Spoofing / Tampering)

**Não mitigado — lacuna real confirmada no código.**
`cmd/tracking-ingest/main.go` `handlePositions` (linhas 89-130) exige JWT
Bearer válido (`issuer.Middleware`), mas **nunca lê `auth.Caller(ctx)`
dentro do handler** e nunca compara contra o dono ou o `courier_id` da
entrega do path. Qualquer portador de um JWT válido — de qualquer
identidade emitida por `POST /auth/tokens` — pode publicar posição GPS
para qualquer `delivery_id`, inclusive entregas atribuídas a outro
entregador.

Nota de modelo de dados: o sistema hoje amarra autorização de tracking ao
conceito de "dono" (`created_by_caller`, quem criou a entrega), não ao
`courier_id` atribuído. Corrigir esta ameaça de verdade exige decidir
_qual identidade_ deve autorizar o envio de posição (o dono? o courier
atribuído, comparado contra `internal/dispatch`?) antes de implementar,
porque hoje o JWT `sub` não carrega papel nem é comparado a `courier_id` em
lugar nenhum do código.

**Ação tomada nesta sessão:** documentar a lacuna com honestidade e
escrever um teste que **prova a lacuna existe** (`TestTracking_
AnyValidCallerCanPostPositionForAnyDelivery` em
`test/integration/tracking_test.go`, marcado como `t.Skip` com o motivo, ou
mantido como teste que hoje passa confirmando o comportamento inseguro —
ver seção de testes). Fechar a lacuna de verdade (adicionar a checagem de
autorização em `tracking-ingest`) é mudança de comportamento de produção
fora do escopo deste passe de auditoria, registrado como pendência
prioritária.

## 3. Replay de token ou comando antigo (Spoofing)

**Parcialmente mitigado.** Todo JWT tem `exp` (linhas 39-43 de
`internal/platform/auth/auth.go`, `jwt.RegisteredClaims.ExpiresAt`),
validado pela biblioteca `golang-jwt/jwt/v5` em `ParseWithClaims`. Um token
expirado é rejeitado.

**Não mitigado:** não há `jti` (JWT ID) nas claims — `IssueToken` nunca
preenche o campo `ID` da struct `jwt.RegisteredClaims` — e não existe
denylist/allowlist de tokens revogados. Um token roubado continua válido e
reutilizável por qualquer um até expirar (TTL configurável, ex.
`time.Hour` em produção). Isso é uma decisão de simplicidade aceita no
tier 2 (JWT HS256 local em vez de OIDC completo, ver
`docs/adr/0005-tokens-assinados-em-vez-de-oidc.md`), não uma correção
esquecida, mas fica documentado aqui como trade-off real, não como
"resolvido".

Replay de _comando_ (não token): a idempotência de `POST /deliveries`
(`Idempotency-Key`) já cobre isso para criação — repetir a mesma chave com
o mesmo payload é idempotente
(`TestDelivery_CreateIsIdempotent`), e a máquina de estados
(`internal/delivery/state.go`) rejeita transições fora de ordem, o que
neutraliza a maior parte do risco prático de "reenviar um comando antigo
por engano" nas transições de lifecycle.

## 4. Adulteração de courier_id, sequência ou timestamp (Tampering)

**Parcialmente mitigado, com uma lacuna real na ingestão.**

- `courier_id`: atribuído pelo `delivery-api`/`internal/fencing` dentro de
  uma transação com disputa concorrente (`INSERT ... ON CONFLICT` em
  `active_assignments`, tier 5), não vem de payload de cliente não
  confiável nesse fluxo. Não há vetor de adulteração direta de
  `courier_id` pelo caller HTTP.
- `sequence`/`epoch` de tracking: **a proteção contra regressão existe na
  projeção, não na ingestão.** `internal/tracking/tracking.go` `RecordPosition`
  usa `WHERE (tracking_session_epoch, sequence) < (EXCLUDED.tracking_session_epoch,
  EXCLUDED.sequence)` — a "posição atual" projetada nunca regride
  (testado em `TestTracking_LatePositionNeverOverridesNewer` e
  `TestTracking_NewEpochSupersedesOldSequence`). Mas o log append-only
  (`tracking_positions`, inserido em `cmd/tracking-ingest`) aceita
  qualquer `(epoch, sequence)` enviado pelo cliente, sem checar
  plausibilidade contra o último epoch/sequence conhecido antes de
  aceitar a escrita — deduplicação é só por `ON CONFLICT DO NOTHING`
  na mesma chave exata, não por ordem.
- `timestamp` (`RecordedAt`): se vier zero, o servidor usa `time.Now()`;
  se vier preenchido, **é aceito sem validação de janela** (não checa se
  está no futuro, nem se é implausivelmente antigo).

**Recomendação registrada, não implementada neste passe:** validar que
`RecordedAt` esteja dentro de uma janela razoável (ex. ±5 minutos do
`time.Now()` do servidor) antes de aceitar a posição. Mudança de
comportamento de produção, fora do escopo deste passe de fechamento de
lacunas de documentação/teste.

## 5. Enumeração de IDs (Information disclosure)

**Parcialmente mitigado.** IDs de entrega e entregador são UUID
(verificado pelo uso de `uuid.New()` no código de criação), não
sequenciais, o que já neutraliza enumeração trivial por incremento. Não
há, no entanto, rate limit dedicado a tentativas de `GET /deliveries/{id}`
com IDs aleatórios no `delivery-api` (sem autenticação nesse endpoint, ver
ameaça 1), então um enumerador com UUIDs vazados de outro canal não
encontraria fricção adicional além do espaço de UUID em si.

## 6. Payload excessivo ou mensagem venenosa (Denial of service)

**Não mitigado para tamanho de payload; mitigado para mensagem
malformada.** `internal/platform/outbox/outbox.go` `Enqueue` não valida
tamanho de payload antes de serializar e inserir no outbox — não há limite
de bytes no código. Para mensagem venenosa (poison pill) já decodificada
do Kafka, `internal/platform/kafka/kafka.go` (`Consumer`, linhas 44-86)
desvia para uma fila DLQ por tópico (`<topico>.dlq`) em vez de travar a
partição, documentado em `docs/runbooks/dlq-replay.md`. O HTTP handler de
tracking (`cmd/tracking-ingest`) usa `http.MaxBytesReader` implícito? não
confirmado neste passe — registrado como pendência de verificação futura,
não afirmado como coberto.

## 7. Credencial exposta em log, trace ou imagem (Information disclosure)

**Mitigado no que foi auditado.** Toda mensagem de log que toca segredo
(`DISPATCH_JWT_SECRET`, `DISPATCH_ADMIN_SECRET`, o segredo resolvido via
`internal/platform/secrets/secretsmanager.go` `ResolveJWTSecret`) loga
apenas o **nome** da variável/segredo (`"secret", secretName`), nunca o
valor. Não foi encontrado nenhum ponto em `internal/` ou `cmd/` que logue
o valor de um token JWT ou segredo. As imagens Docker (`deploy/compose/Dockerfile.*`)
são multi-stage, binário estático sobre base mínima, sem segredo
embutido em build args versionados (segredos são injetados via variável de
ambiente em runtime, não em `ARG`/`ENV` no `Dockerfile`).

**Verificação não exaustiva:** não foi auditado se algum `err.Error()`
propagado de uma chamada HTTP client externa poderia ecoar um header
sensível de volta ao log; fica como possibilidade não descartada, não
como risco confirmado.

## 8. Coordenada mantida além do prazo (Information disclosure / retenção)

**Não mitigado, sem TTL implementado.** `tracking_positions` é uma tabela
append-only sem rotina de expurgo automatizado encontrada no código
(`migrations/`, `internal/tracking/`). Redis (projeção de posição atual,
ver `docs/adr/0003-redis-como-projecao.md`) tem TTL configurável por
natureza do Redis, mas não foi confirmado neste passe se a chave de
posição atual usa `EXPIRE`. Registrado como pendência real: um sistema de
produção precisaria de política de retenção (ex. purgar histórico de
posição após N dias), que este laboratório não implementa.

## 9. Serviço comprometido acessando recurso desnecessário (Elevation of privilege)

**Parcialmente mitigado por design, não por controle de acesso em tempo de
execução.** Cada `cmd/` (delivery-api, dispatch-worker, tracking-ingest,
tracking-projector, notification-worker) roda como processo/container
separado com sua própria credencial de banco (`DATABASE_URL`) e grupo de
consumidor Kafka próprio — isolamento por processo, não por permissão de
banco (não há usuário Postgres distinto por serviço com `GRANT` restrito
por tabela; todos usam o mesmo usuário `dispatch` no `docker-compose.yml`
local). Em produção real isso pediria usuários de banco por serviço com
menor privilégio; não implementado neste laboratório, documentado aqui
como gap consciente e proporcional ao escopo (custo de manter N usuários
Postgres/roles num ambiente de estudo não se paga).

## 10. Imagem ou dependência alterada na supply chain (Tampering)

**Mitigado, com evidência real.** `docs/adr/0016-sbom-scan-e-assinatura-de-imagem.md`
documenta SBOM real (`syft` v1.49.0, SPDX JSON) e scan de vulnerabilidade
real (`grype` v0.116.0, nenhuma vulnerabilidade encontrada nas 5 imagens
na execução registrada) para as imagens do `docker compose`, e assinatura
real com `cosign` v2.4.1 (chave local, não keyless) para
`dispatch-delivery-api`, verificada com `cosign verify` contra um registry
OCI local efêmero. As outras 4 imagens têm SBOM/scan mas não foram
assinadas (pendência menor documentada no próprio ADR). Nesta sessão,
`.github/workflows/supply-chain.yaml` automatiza SBOM e scan em CI
(assinatura fica documentada, não executada em CI, ver
`docs/adr/0024-ci-github-actions-e-onde-ficam-os-templates.md`).
Dependabot (`.github/dependabot.yml`) mantém módulos Go, imagens base e
GitHub Actions atualizados contra CVE conhecido.

## Testes negativos adicionados nesta sessão

- `internal/delivery/authorization_test.go` —
  `TestAuthorizeOwner_RejectsCallerThatIsNotOwner` e
  `TestAuthorizeOwner_AllowsOwner`: testes de unidade contra a lógica de
  `authorizeOwner` extraída/exercitada isoladamente (dado um dono e um
  caller diferentes, nega; dado o mesmo caller, permite). Ver arquivo para
  a forma exata usada, dado que `authorizeOwner` em si é um método não
  exportado de `cmd/tracking-projector`; o teste cobre o mesmo contrato via
  `test/invariant/`, ver abaixo.
- `test/invariant/authorization_invariant_test.go` — invariante central:
  "um caller nunca lê tracking de uma entrega que não criou", exercitando
  o fluxo HTTP real do `tracking-projector` contra Postgres real (mesmo
  padrão de `test/integration`), com asserção de `403 Forbidden`.

## O que este documento não cobre

Ameaças de infraestrutura de nuvem real (IAM da AWS, VPC, security group)
não se aplicam: este projeto não roda contra conta de nuvem paga (ver
`docs/limitacoes-simulacao-local.md`). Ameaças de disponibilidade sob carga
adversária extrema (DDoS distribuído) não foram modeladas: o rate limit
existente é por identidade autenticada, pensado para abuso de cliente
legítimo, não para um ataque de negação de serviço distribuído.
