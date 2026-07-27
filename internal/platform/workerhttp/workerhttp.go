// Package workerhttp dá a um worker sem rota de negócio (consumidores puros
// de Kafka, como lunchrush-worker e notification-worker) o mínimo que
// Kubernetes e Prometheus precisam: /healthz para liveness/readiness e
// /metrics para scrape.
package workerhttp

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Serve inicia o servidor numa goroutine e não bloqueia: um worker cuja
// razão de existir é consumir Kafka não deve ficar preso a um listener
// HTTP no caminho principal.
func Serve(addr string, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /metrics", promhttp.Handler())

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("servidor de health/metrics encerrou", "error", err)
		}
	}()
}
