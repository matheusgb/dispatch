// Package idempotency implementa o ledger de chaves de idempotência
// (invariante 5 e 6 do tier 1): repetir uma requisição com a mesma chave e o
// mesmo payload produz um único efeito de negócio; a mesma chave com um
// payload diferente é rejeitada como conflito.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrConflict indica que a chave já foi usada com um payload diferente.
var ErrConflict = errors.New("idempotency: mesma chave com payload diferente")

// Key identifica uma operação de forma única por chamador.
type Key struct {
	Caller    string
	Operation string
	Value     string
}

// HashPayload calcula o hash canônico usado para detectar reuso divergente.
func HashPayload(payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Result é o que fica persistido e pode ser devolvido em uma repetição.
type Result struct {
	StatusCode int
	Body       []byte
}

// Run garante que fn execute no máximo uma vez para a mesma Key e o mesmo
// payloadHash, dentro da transação tx. Uma repetição com o mesmo payload
// devolve o resultado gravado sem chamar fn de novo; uma repetição com
// payload diferente devolve ErrConflict sem chamar fn.
// Run devolve replayed=true quando o resultado veio do ledger em vez de uma
// execução nova de fn: quem chama usa isso para não contar duas vezes uma
// métrica de negócio que já foi contada na primeira execução.
func Run(ctx context.Context, tx pgx.Tx, k Key, payloadHash string, ttl time.Duration, fn func(ctx context.Context) (Result, error)) (Result, bool, error) {
	var existingHash string
	var res Result
	err := tx.QueryRow(ctx, `
		SELECT payload_hash, status_code, response_body
		FROM idempotency_keys
		WHERE caller = $1 AND operation = $2 AND key = $3
	`, k.Caller, k.Operation, k.Value).Scan(&existingHash, &res.StatusCode, &res.Body)

	switch {
	case err == nil:
		if existingHash != payloadHash {
			return Result{}, false, ErrConflict
		}
		return res, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		// segue para executar fn e gravar o resultado.
	default:
		return Result{}, false, err
	}

	res, err = fn(ctx)
	if err != nil {
		return Result{}, false, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (caller, operation, key, payload_hash, status_code, response_body, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7))
	`, k.Caller, k.Operation, k.Value, payloadHash, res.StatusCode, res.Body, ttl.Seconds())
	if err != nil {
		return Result{}, false, err
	}
	return res, false, nil
}
