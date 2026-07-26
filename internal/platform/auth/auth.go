// Package auth implementa a autenticação simplificada do tier 2: tokens
// assinados (JWT HS256), sem servidor OIDC. O roadmap aceita essa
// alternativa explicitamente ("OIDC local ou tokens assinados"); um
// provedor OIDC de verdade fica para quando houver motivo medido para a
// complexidade extra de um servidor de identidade.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrMissingToken indica que a requisição não trouxe um Authorization
// Bearer válido.
var ErrMissingToken = errors.New("auth: token ausente ou malformado")

type contextKey string

const callerKey contextKey = "auth_caller"

type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

// IssueToken emite um token de curta duração para o caller informado. No
// tier 2, quem chama isto é um endpoint administrativo protegido por um
// segredo compartilhado, não um fluxo de login de usuário final.
func (i *Issuer) IssueToken(caller string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   caller,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(i.ttl)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(i.secret)
}

func (i *Issuer) parse(raw string) (string, error) {
	token, err := jwt.ParseWithClaims(raw, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		return i.secret, nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return "", ErrMissingToken
	}
	return claims.Subject, nil
}

// Middleware exige um Authorization: Bearer <token> válido e injeta o
// caller no contexto da requisição. Handlers downstream leem com Caller.
func (i *Issuer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(raw, prefix) {
			http.Error(w, `{"error":"token ausente"}`, http.StatusUnauthorized)
			return
		}
		caller, err := i.parse(strings.TrimPrefix(raw, prefix))
		if err != nil {
			http.Error(w, `{"error":"token inválido"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), callerKey, caller)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Caller devolve o identificador autenticado da requisição, ou "" se o
// contexto não passou pelo Middleware.
func Caller(ctx context.Context) string {
	c, _ := ctx.Value(callerKey).(string)
	return c
}
