# ADR 0016: SBOM, scan de vulnerabilidades e assinatura de imagem com chave local

## Contexto

O critério de conclusão do tier 4 pedia SBOM, scan de dependências e imagem,
e assinatura com proveniência. Uma sessão anterior fechou o tier 4 sem esse
item, por restrição de tempo e memória compartilhada com o `edge-lab`. Este
ADR fecha a lacuna antes de começar o tier 5, com ferramentas reais rodando
contra as imagens já buildadas do `docker compose` (`lunchrush-delivery-api`,
`lunchrush-lunchrush-worker`, `lunchrush-tracking-ingest`,
`lunchrush-tracking-projector`, `lunchrush-notification-worker`).

## Decisão

- **SBOM:** `syft` (v1.49.0, instalado localmente via script oficial,
  binário fora do repositório), formato SPDX JSON, uma execução por
  imagem. Saída em `docs/benchmarks/supply-chain/<imagem>-sbom.spdx.json`.
- **Scan de vulnerabilidades:** `grype` (v0.116.0), banco de dados de
  vulnerabilidades público baixado na primeira execução, contra cada
  imagem. Saída em `docs/benchmarks/supply-chain/<imagem>-grype.txt`.
- **Assinatura:** `cosign` (v2.4.1) com **par de chaves local**
  (`cosign generate-key-pair`, chave privada protegida por senha vazia só
  neste laboratório efêmero, nunca comitada com senha real em produção),
  não o fluxo keyless/Sigstore público que depende de um provedor OIDC
  público (GitHub Actions OIDC, Google, etc.) inexistente neste laboratório
  local. Assinar exige empurrar a assinatura como artefato OCI para um
  registry; como não há ECR nem outro registry real neste laboratório, foi
  usado um `registry:2` local efêmero (`localhost:5555`, container Docker
  descartado depois da evidência).

## O que realmente aconteceu (evidência, não simulação)

```text
$ syft lunchrush-delivery-api:latest -o spdx-json | wc -c
736742
# 27 pacotes catalogados (SPDX "packages"), incluindo o próprio módulo
# github.com/matheusgb/lunch-rush e as dependências diretas (pgx, kafka-go,
# jwt/v5, prometheus client, etc). Contagem análoga nas outras 4 imagens
# (21 a 30 pacotes, a diferença reflete o import graph de cada cmd/).

$ grype lunchrush-delivery-api:latest
No vulnerabilities found
# mesmo resultado nas 5 imagens: esperado, são binários Go estáticos sobre
# alpine mínimo (ver Dockerfile multi-stage de cada cmd/), superfície de
# pacotes de sistema operacional é pequena.

$ docker tag lunchrush-delivery-api:latest localhost:5555/lunchrush-delivery-api:latest
$ docker push localhost:5555/lunchrush-delivery-api:latest
latest: digest: sha256:6e6d6da424f66c07ead8273b6f8a2cfca404331ad18e4c36e6d5bd6130613fe3

$ cosign sign --key cosign.key --allow-http-registry --yes \
    localhost:5555/lunchrush-delivery-api:latest
tlog entry created with index: 2256576409
Pushing signature to: localhost:5555/lunchrush-delivery-api

$ cosign verify --key cosign.pub --allow-http-registry --insecure-ignore-tlog=true \
    localhost:5555/lunchrush-delivery-api:latest
Verification for localhost:5555/lunchrush-delivery-api:latest --
The following checks were performed on each of these signatures:
  - The cosign claims were validated
  - The signatures were verified against the specified public key
```

Saída bruta completa em `docs/benchmarks/supply-chain/` (SBOMs, `*-grype.txt`,
`cosign-verify.json`, `cosign-verify.log`). `cosign.key`/`cosign.pub` ficam
no `.gitignore` local por serem material de assinatura, mesmo sendo uma
chave de laboratório sem valor de produção; o `cosign.pub` fica versionado
para permitir reproduzir a verificação.

## Nota honesta sobre o transparency log

O comando `cosign sign` sem `--tlog-upload=false` registrou a assinatura no
Rekor público (transparency log hospedado pela Sigstore), mesmo usando
chave local em vez de identidade OIDC. Isso não é um recurso AWS nem gera
custo, mas é uma chamada de rede para um serviço público de terceiros fora
do laboratório. Sessões futuras que quiserem evitar qualquer chamada de
rede pública devem usar `--tlog-upload=false` na assinatura e
`--insecure-ignore-tlog=true` na verificação (usado aqui só para
verificação, já que a assinatura já havia ido ao log público).

## Alternativas consideradas

- **Sigstore keyless (OIDC):** rejeitada porque exigiria uma identidade
  federada real (conta GitHub/Google), fora do escopo de um laboratório
  local sem pipeline de CI real publicando para um registry real.
- **Não assinar nada, só documentar a intenção:** rejeitada pelo mesmo
  motivo do ADR 0012: texto sem execução não prova nada.
- **Trivy em vez de Grype:** ambos maduros; Grype escolhido por já vir do
  mesmo mantenedor do Syft (Anchore), com integração de SBOM SPDX direta.

## Consequências

- as 5 imagens do `docker compose` têm SBOM e scan real capturados neste
  commit; nenhuma vulnerabilidade encontrada nesta execução (não é garantia
  permanente: o banco do Grype muda com o tempo);
- só `lunchrush-delivery-api` foi de fato assinada e verificada (prova de
  conceito do fluxo completo); replicar para as outras 4 é mecânico e fica
  registrado como pendência menor em
  `docs/benchmarks/tier-4-what-breaks-next.md`;
- push para ECR por identidade federada (OIDC do GitHub Actions, como pede
  o roadmap do tier 4) continua fora de alcance sem conta AWS real; ver
  `docs/limitacoes-simulacao-local.md`.

## Status

Aceita.
