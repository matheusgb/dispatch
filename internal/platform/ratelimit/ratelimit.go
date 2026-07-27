// Package ratelimit aplica um token bucket por cliente autenticado, usando
// golang.org/x/time/rate: a biblioteca padrão de fato para isso em Go, não
// há motivo para reimplementar o algoritmo.
package ratelimit

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/matheusgb/lunch-rush/internal/platform/auth"
)

type PerCaller struct {
	rps   rate.Limit
	burst int

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func NewPerCaller(rps float64, burst int) *PerCaller {
	return &PerCaller{rps: rate.Limit(rps), burst: burst, limiters: make(map[string]*rate.Limiter)}
}

func (p *PerCaller) limiterFor(caller string) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	l, ok := p.limiters[caller]
	if !ok {
		l = rate.NewLimiter(p.rps, p.burst)
		p.limiters[caller] = l
	}
	return l
}

// Middleware precisa correr depois de auth.Issuer.Middleware: o rate limit
// é por identidade autenticada, não por IP, porque em produção várias
// requisições legítimas passam pelo mesmo load balancer ou NAT.
func (p *PerCaller) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := auth.Caller(r.Context())
		if caller == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !p.limiterFor(caller).Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"limite de requisições excedido"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
