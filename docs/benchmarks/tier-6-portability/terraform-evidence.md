# Evidência real: Terraform separado por provedor (cloud-a, cloud-b)

Execução real em 2026-07-26, `terraform` 1.x, provider `hashicorp/aws` 5.100.0
(instalado via filesystem mirror local, sem acesso à internet nesta máquina;
o pacote já estava em cache de uma sessão anterior do tier 4).

## cloud-a (`infra/terraform/environments/cloud-a`, LocalStack porta 4566)

```
$ terraform init && terraform apply -auto-approve
Apply complete! Resources: 8 added, 0 changed, 0 destroyed.
jwt_secret_arn = "arn:aws:secretsmanager:us-east-1:000000000000:secret:lunchrush/cloud-a/jwt-secret-eiZeXU"
jwt_secret_name = "lunchrush/cloud-a/jwt-secret"
receipts_bucket_name = "lunchrush-cloud-a-receipts"
```

## cloud-b (`infra/terraform/environments/cloud-b`, LocalStack porta 14566)

```
$ terraform init && terraform apply -auto-approve
Apply complete! Resources: 8 added, 0 changed, 0 destroyed.
jwt_secret_arn = "arn:aws:secretsmanager:us-east-1:000000000000:secret:lunchrush/cloud-b/jwt-secret-vMmlpR"
jwt_secret_name = "lunchrush/cloud-b/jwt-secret"
receipts_bucket_name = "lunchrush-cloud-b-receipts"
```

Confirmado por HTTP direto que os dois buckets existem em dois LocalStack
independentes (dois containers, duas redes Docker, dois processos), não um
único LocalStack compartilhado:

```
$ curl -s http://localhost:4566/lunchrush-cloud-a-receipts/
<ListBucketResult ... Name>lunchrush-cloud-a-receipts</Name> ...>
$ curl -s http://localhost:14566/lunchrush-cloud-b-receipts/
<ListBucketResult ... Name>lunchrush-cloud-b-receipts</Name> ...>
```

## Destroy e auditoria

```
$ terraform destroy -auto-approve   # cloud-a
Destroy complete! Resources: 8 destroyed.
$ terraform destroy -auto-approve   # cloud-b
Destroy complete! Resources: 8 destroyed.
```

Saídas completas dos dois `apply` (JSON de outputs, antes do destroy):
`terraform-cloud-a-outputs.json`, `terraform-cloud-b-outputs.json`.

## Limitação honesta

Isto prova que o padrão "um root Terraform por provedor" (lunch-rush.md,
tier 6) funciona mecanicamente duas vezes, contra dois LocalStack
independentes representando duas contas/projetos diferentes. Não prova:

- duas contas AWS reais (seria a regra não-negociável do projeto sendo
  violada);
- IAM, rede, DNS ou billing reais de um segundo provedor;
- nenhum serviço além de S3 e Secrets Manager/KMS, porque LocalStack
  community não implementa o resto (mesma limitação do tier 4, ADR 0012).
