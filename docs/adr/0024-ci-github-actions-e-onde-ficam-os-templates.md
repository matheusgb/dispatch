# ADR 0024: CI no GitHub Actions sem testes de integração, e templates em docs/templates/

## Contexto

O roadmap (`lunch-rush.md`) pedia CI real (lint, `go vet`, `go test -race`,
build de imagem, SBOM/scan) e templates de ADR, experimento, incidente e
benchmark. Uma auditoria encontrou `.github/` inteiramente ausente. Este
projeto é laboratório solo (ver decisão de não usar PRs/issues/branch
protection, registrada só em conversa, não neste ADR, por ser processo e
não arquitetura): o CI aqui documenta e valida sintaxe, não gira num
pipeline de equipe.

## Decisão

- `.github/workflows/ci.yaml`: `gofmt -l`, `go vet`, `go build`, `go test
  -race` em cada push para `master` e em pull request; um job de matriz que
  builda a imagem Docker de cada `cmd/` (sem push), para pegar Dockerfile
  quebrado cedo.
- `.github/workflows/supply-chain.yaml`: reaproveita os comandos já
  validados manualmente no tier 4 (`docs/adr/0016-sbom-scan-e-assinatura-de-imagem.md`)
  com `syft`/`grype` via as actions oficiais (`anchore/sbom-action`,
  `anchore/scan-action`), rodando semanalmente e a cada push. O job de
  assinatura com `cosign` fica desabilitado (`if: false`) e documentado
  como próximo passo: assinar de verdade só faz sentido contra um registry
  real, que este laboratório não publica a partir de CI automático (evita
  gerar entrada real no transparency log público do Rekor sem necessidade).
- **Testes de integração (`test/integration/`, que exigem Postgres e
  Kafka via `docker compose`) não rodam neste CI.** Rodar `docker compose`
  dentro de um runner hospedado do GitHub Actions é possível, mas este
  laboratório nunca validou isso (o ambiente de desenvolvimento sempre foi
  local, com docker compose já de pé). Adicionar isso sem testar seria o
  mesmo erro do `terraform apply` que ficou pendurado contra o LocalStack:
  documentar sem executar não prova nada. Fica registrado como pendência
  honesta, não como "feito".
- **Templates de ADR, experimento, incidente e benchmark ficam em
  `docs/templates/`**, não em `.github/ISSUE_TEMPLATE/`. Como o projeto não
  usa o fluxo de Issues do GitHub (decisão do mantenedor: laboratório solo,
  sem processo de PR/issue/branch protection), um template de Issue Form
  nunca seria de fato preenchido por ninguém; um arquivo Markdown em
  `docs/templates/` é copiado manualmente ao criar `docs/adr/00NN-*.md`,
  `docs/benchmarks/*.md` etc, que é como todo documento deste tipo já é
  criado no repositório.

## Alternativas consideradas

- **Testar os workflows com `act` localmente:** feito. Ver nota abaixo.
- **GitHub Actions completo com testes de integração via `services:`
  (containers Postgres/Kafka nativos do runner):** rejeitado por agora, ver
  decisão acima.

## Nota sobre `act`

Sessão anterior não tinha `act` instalado e revisou os workflows só
manualmente. Esta sessão instalou `act` v0.2.89 (script oficial
`nektos/act`, sem apt/sudo) e rodou os dois workflows de verdade contra o
runner `catthehacker/ubuntu:act-latest`: evidência completa em
`docs/benchmarks/act-ci-local.txt`.

Resultado resumido: `ci.yaml` (`lint-vet-test` e a matriz de 8 imagens em
`build-images`) passou 100% real, sem simulação. A revisão via `act -l`
encontrou um bug real (referência a um ADR com nome de arquivo errado no
comentário do workflow), corrigido nesta sessão. `supply-chain.yaml`
(`sbom-and-scan`) passou 100% real quando rodado com um serviço da matriz
por vez; rodar a matriz de 5 inteira expõe duas limitações conhecidas do
`act` (não do workflow): corrida de tool-cache local entre jobs paralelos
e falta do backend de upload de artefato sem `--artifact-server-path`. O
job `cosign-sign` (`if: false`) foi confirmado pulado pelo `act`, igual ao
GitHub Actions real, provando que a condição tem o efeito pretendido.
`act` de verdade não substitui rodar no GitHub Actions hospedado (algumas
particularidades do runner hospedado, como cache entre execuções e
segredos reais, não são replicadas), mas valida a lógica de cada step
muito além de uma revisão de sintaxe.

## Consequências

- todo push/PR passa por lint/vet/test/build automaticamente quando este
  repositório for hospedado no GitHub com Actions habilitado;
- SBOM e scan rodam semanalmente sem depender de alguém lembrar de rodar
  `syft`/`grype` manualmente;
- testes de integração, e2e e contract continuam exigindo execução local
  documentada (`make test-integration`, `make e2e`, `make contract-test`);
  replicar isso em CI hospedado é trabalho futuro, não coberto aqui.

## Status

Aceita.
