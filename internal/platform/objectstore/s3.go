// Package objectstore guarda comprovantes de entrega concluída em um bucket
// S3. No laboratório, "S3" é o LocalStack (ver docker-compose.yml, profile
// aws-lab, e infra/terraform/environments/aws-lab): mesmo protocolo e
// mesmo SDK que apontariam para a AWS real, só o endpoint muda.
//
// A subida do comprovante é best-effort: o efeito de negócio da entrega
// (transição para "delivered") já está confirmado no Postgres antes de
// qualquer chamada aqui. Se o S3/LocalStack estiver fora do ar, a entrega
// continua concluída e o único efeito colateral é a ausência do
// comprovante, registrada em log e numa métrica — nunca uma falha do
// endpoint de negócio.
package objectstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Receipt é o comprovante de entrega salvo como objeto JSON.
type Receipt struct {
	DeliveryID  string    `json:"delivery_id"`
	CourierID   string    `json:"courier_id,omitempty"`
	State       string    `json:"state"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type Client struct {
	s3     *s3.Client
	bucket string
	logger *slog.Logger
}

// New cria um client S3 apontando para endpoint (LocalStack em
// docker-compose.yml, "http://localstack:4566" dentro da rede do compose).
// Se endpoint for vazio, o cliente fica desabilitado: Put vira um no-op
// registrado em log, para que o serviço continue funcionando sem a
// substituição local do S3 configurada (por exemplo, fora do profile
// aws-lab).
func New(ctx context.Context, endpoint, region, bucket string, logger *slog.Logger) (*Client, error) {
	if endpoint == "" {
		return &Client{logger: logger}, nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return nil, fmt.Errorf("carregar config aws: %w", err)
	}
	cli := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // LocalStack não resolve DNS por bucket subdomain
	})
	return &Client{s3: cli, bucket: bucket, logger: logger}, nil
}

// Enabled indica se há um bucket configurado.
func (c *Client) Enabled() bool { return c != nil && c.s3 != nil }

// EnsureBucket cria o bucket se não existir. Idempotente: BucketAlreadyOwnedByYou
// e BucketAlreadyExists são tratados como sucesso.
func (c *Client) EnsureBucket(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	_, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)})
	if err != nil {
		var alreadyOwned *s3types.BucketAlreadyOwnedByYou
		var alreadyExists *s3types.BucketAlreadyExists
		if errors.As(err, &alreadyOwned) || errors.As(err, &alreadyExists) {
			return nil
		}
		// LocalStack retorna erro genérico quando o bucket já existe;
		// checar de novo evita falhar o startup por uma corrida de criação.
		if _, headErr := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)}); headErr == nil {
			return nil
		}
		return fmt.Errorf("criar bucket %s: %w", c.bucket, err)
	}
	return nil
}

// PutReceipt sobe o comprovante como objeto
// "receipts/<delivery_id>/<unix_nano>.json".
func (c *Client) PutReceipt(ctx context.Context, r Receipt) error {
	if !c.Enabled() {
		if c.logger != nil {
			c.logger.Debug("objectstore desabilitado, comprovante não persistido", "delivery_id", r.DeliveryID)
		}
		return nil
	}
	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("serializar comprovante: %w", err)
	}
	key := fmt.Sprintf("receipts/%s/%d.json", r.DeliveryID, r.DeliveredAt.UnixNano())
	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("subir comprovante %s: %w", key, err)
	}
	return nil
}
