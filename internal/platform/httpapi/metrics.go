package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Métricas RED (rate, errors, duration). As labels são rota e método, nunca
// delivery_id ou courier_id: isso causaria cardinalidade sem limite, como o
// tier 1 do roadmap explicitamente proíbe.
var (
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dispatch_http_requests_total",
		Help: "Total de requisições HTTP por rota, método e status.",
	}, []string{"route", "method", "status"})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dispatch_http_request_duration_seconds",
		Help:    "Duração das requisições HTTP por rota e método.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	// Métricas de negócio: contadores agregados, sem identificador de
	// entrega ou entregador.
	deliveriesCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dispatch_deliveries_created_total",
		Help: "Total de entregas criadas.",
	})
	deliveriesAssignedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dispatch_deliveries_assigned_total",
		Help: "Total de atribuições de entregador confirmadas.",
	})
	assignmentConflictsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dispatch_assignment_conflicts_total",
		Help: "Total de tentativas de atribuição rejeitadas por disputa concorrente.",
	})
	deliveriesCompletedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dispatch_deliveries_completed_total",
		Help: "Total de entregas concluídas (picked_up -> delivered).",
	})
)

func init() {
	prometheus.MustRegister(
		requestsTotal, requestDuration,
		deliveriesCreatedTotal, deliveriesAssignedTotal, assignmentConflictsTotal, deliveriesCompletedTotal,
	)
}

// metricsMiddleware usa r.Pattern (Go 1.22+) como label de rota: é o padrão
// registrado no ServeMux ("/deliveries/{id}"), não o path concreto, então a
// cardinalidade fica limitada ao número de rotas.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		requestsTotal.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		requestDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}
