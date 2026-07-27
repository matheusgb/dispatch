package notification

import "github.com/prometheus/client_golang/prometheus"

// notificationsTotal usa "kind" (tipo de evento) e "outcome" (sent, rejected,
// error) como labels, nunca delivery_id: a mesma disciplina de cardinalidade
// do tier 1 continua valendo.
var notificationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "lunchrush_notifications_total",
	Help: "Total de notificações transacionais enviadas ao provedor simulado, por tipo e resultado.",
}, []string{"kind", "outcome"})

func init() {
	prometheus.MustRegister(notificationsTotal)
}
