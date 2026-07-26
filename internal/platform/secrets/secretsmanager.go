// Package secrets busca o segredo do JWT no AWS Secrets Manager (LocalStack
// no laboratório) na inicialização do serviço, com fallback documentado
// para variável de ambiente quando o Secrets Manager não está configurado
// ou não responde. O fallback existe porque o tier 3 já funciona só com
// DISPATCH_JWT_SECRET em env var, e o tier 4 não pode quebrar esse modo de
// operação: LocalStack é um profile opcional (aws-lab), não uma dependência
// obrigatória do compose padrão.
package secrets

import (
	"context"
	"log/slog"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// ResolveJWTSecret tenta buscar secretName no Secrets Manager apontado por
// endpoint. Em qualquer falha (endpoint vazio, serviço fora do ar, segredo
// inexistente), devolve envFallback e registra o motivo em log: o startup
// nunca falha só por causa do Secrets Manager simulado.
func ResolveJWTSecret(ctx context.Context, endpoint, region, secretName, envFallback string, logger *slog.Logger) string {
	if endpoint == "" {
		if logger != nil {
			logger.Info("secrets manager não configurado, usando DISPATCH_JWT_SECRET diretamente")
		}
		return envFallback
	}
	if envFallback == "" {
		if logger != nil {
			logger.Warn("nenhum fallback de env var configurado para o JWT secret; se o secrets manager falhar, o startup falhará")
		}
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		if logger != nil {
			logger.Warn("carregar config aws para secrets manager, usando fallback", "error", err)
		}
		return envFallback
	}

	cli := secretsmanager.NewFromConfig(cfg, func(o *secretsmanager.Options) {
		o.BaseEndpoint = &endpoint
	})

	out, err := cli.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &secretName})
	if err != nil {
		if logger != nil {
			logger.Warn("buscar segredo no secrets manager, usando fallback", "secret", secretName, "error", err)
		}
		return envFallback
	}
	if out.SecretString == nil || *out.SecretString == "" {
		if logger != nil {
			logger.Warn("segredo vazio no secrets manager, usando fallback", "secret", secretName)
		}
		return envFallback
	}
	if logger != nil {
		logger.Info("jwt secret carregado do secrets manager", "secret", secretName)
	}
	return *out.SecretString
}
