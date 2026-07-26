# ADR 0012: Terraform contra LocalStack no ambiente aws-lab

## Contexto

O tier 4 pede infraestrutura provisionada com Terraform contra uma conta
AWS real. Este laboratório não usa conta AWS real nem gasta dinheiro (ver
`docs/limitacoes-simulacao-local.md`). A pergunta que este ADR responde é
mais estreita: dado que a AWS real está fora de escopo, vale a pena rodar
Terraform de verdade contra alguma coisa, ou o Terraform inteiro deveria
ficar só como texto documentando intenção?

## Decisão

Rodar Terraform de verdade contra o LocalStack (`localstack/localstack:3.8.1`,
serviço `localstack` em `docker-compose.yml`, profile `aws-lab`, porta
4566), usando o provider `hashicorp/aws` oficial sem nenhum fork ou
provider alternativo. A única diferença de configuração em relação a uma
conta AWS real é o bloco `endpoints {}` do provider, apontando cada
serviço emulado para `http://localhost:4566`, e credenciais estáticas
`test`/`test` (aceitas pelo LocalStack, nunca usadas contra a AWS real).

Estrutura:

```text
infra/terraform/
  bootstrap/                 # bucket S3 + tabela DynamoDB que seriam o
                              # backend remoto, criados para provar o padrão
  modules/
    storage/                 # bucket S3 de comprovantes de entrega
    secrets/                 # chave KMS + segredo do JWT no Secrets Manager
  environments/
    aws-lab/                 # environment que compõe os módulos acima
```

Escopo do que entra: apenas os serviços que o LocalStack community
(edição gratuita, sem licença Pro) implementa de forma honesta e que o
tier 4 já precisa de verdade: S3 e Secrets Manager (com KMS por baixo,
usado pelo Secrets Manager para cifrar o segredo). EKS, MSK, RDS/Aurora e
ElastiCache **não** têm módulo aqui, porque o LocalStack community não os
implementa (ficam atrás do LocalStack Pro pago) e fingir um `aws_msk_cluster`
que nunca aceita uma conexão Kafka de verdade seria pior que não ter o
módulo.

## Estado local, não remoto

O `environment/aws-lab` usa estado local (`terraform.tfstate` no próprio
diretório, no `.gitignore`), não o backend S3+DynamoDB criado em
`bootstrap/`. `bootstrap/` existe só para provar que o padrão de bootstrap
funciona de verdade contra a API do LocalStack (o bucket e a tabela são
criados e existem), mas encadear esse backend no `aws-lab` trocaria um
problema real (perda de estado) por uma falsa sensação de backend durável:
o LocalStack community sobe com `PERSISTENCE=0` neste laboratório (ver
`docker-compose.yml`), então qualquer estado dentro dele desaparece a cada
`docker compose down`. Um backend remoto que não sobrevive a um restart do
container que o hospeda não é backend remoto, é só mais um jeito de perder
o `.tfstate`.

## Limitação encontrada: `aws_s3_bucket_lifecycle_configuration` trava o apply

Durante a validação deste ADR, `terraform apply` no módulo `storage` ficou
preso indefinidamente no recurso `aws_s3_bucket_lifecycle_configuration`.
O log do LocalStack mostra a causa: o provider chama
`GetBucketLifecycleConfiguration` repetidamente (a cada ~5s) esperando a
configuração convergir, o LocalStack responde `200` a cada chamada, mas o
waiter do provider nunca considera o resultado como propagado e o `apply`
nunca termina (nem falha, nem completa). Confirmado batendo o log:

```text
AWS s3.GetBucketLifecycleConfiguration => 200   (repetido indefinidamente)
```

Resolução: o recurso foi removido de `infra/terraform/modules/storage/main.tf`
(a intenção, expirar versões antigas de comprovante em 30 dias, ficou
documentada em comentário no módulo, não aplicada). Todos os demais
recursos do módulo (`aws_s3_bucket`, `aws_s3_bucket_versioning`,
`aws_s3_bucket_server_side_encryption_configuration`,
`aws_s3_bucket_public_access_block`) aplicaram e validaram normalmente.
Isso já havia derrubado uma sessão anterior de trabalho neste laboratório
(o comando ficou pendurado sem limite de tempo); a lição prática, também
registrada no processo deste tier, é nunca rodar `terraform apply` (ou
qualquer comando potencialmente longo) sem um timeout explícito.

## Evidência

Depois do apply limpo (`terraform plan` reporta "No changes"), contra o
LocalStack real rodando no host:

```text
$ aws --endpoint-url=http://localhost:4566 s3 ls
2026-07-26 17:58:11 dispatch-delivery-receipts

$ aws --endpoint-url=http://localhost:4566 s3api get-bucket-versioning --bucket dispatch-delivery-receipts
{"Status": "Enabled"}

$ aws --endpoint-url=http://localhost:4566 secretsmanager list-secrets
{"SecretList": [{"Name": "dispatch/jwt-secret", "KmsKeyId": "301ad61a-...", ...}]}

$ aws --endpoint-url=http://localhost:4566 kms list-aliases
{"Aliases": [{"AliasName": "alias/dispatch-aws-lab-jwt", ...}]}
```

O bucket e o segredo criados pelo Terraform são os mesmos que
`internal/platform/objectstore` e `internal/platform/secrets` usam em
runtime (mesmo nome de bucket, mesmo nome de segredo, mesmo endpoint),
fechando o ciclo: infraestrutura declarada em Terraform, consumida pelo
serviço Go, sem configuração duplicada entre os dois.

## Alternativas consideradas

- **Terraform só como texto, nunca aplicado:** rejeitada. Não prova nada
  além de sintaxe válida; o objetivo do tier é também exercitar o
  ciclo de vida real (`init`, `plan`, `apply`, drift, `state list`).
- **`tflocal` (wrapper da própria LocalStack para reescrever endpoints
  automaticamente):** não usado porque não estava disponível no ambiente e
  o bloco `endpoints {}` explícito no provider já é portável: é a mesma
  sintaxe que apontaria para a AWS real, só trocando os valores por
  variável de ambiente ou `.tfvars` no dia em que este laboratório
  eventualmente apontar para uma conta real (fora de escopo aqui).

## Consequências

- todo número e todo recurso citado como "criado" neste laboratório foi de
  fato criado contra uma API real (a do LocalStack), não simulado em texto;
- a lista de serviços cobertos (S3, Secrets Manager, KMS) é
  deliberadamente menor que o roadmap do tier 4 pede; a lacuna (EKS, MSK,
  RDS, ElastiCache) está registrada em `docs/limitacoes-simulacao-local.md`,
  não escondida;
- `aws_s3_bucket_lifecycle_configuration` fica marcado como recurso que não
  funciona de forma confiável contra este LocalStack 3.8.1; se uma versão
  futura corrigir o waiter, o recurso pode voltar ao módulo.

## Status

Aceita.
