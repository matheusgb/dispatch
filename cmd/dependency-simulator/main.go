// Comando dependency-simulator finge ser o provedor externo de
// notificações. Existe para testar como o dispatch se comporta quando uma
// dependência de terceiro degrada: 429, timeout e 5xx controlados, sem
// depender de um provedor real (que o roadmap explicitamente proíbe usar
// aqui, ver P00 do escopo: nada de dados reais de pessoas ou serviço real).
package main

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := envOr("HTTP_ADDR", ":8090")
	failureRate := envFloat("FAILURE_RATE", 0.0)
	timeoutRate := envFloat("TIMEOUT_RATE", 0.0)
	latencyMs := envInt("LATENCY_MS", 0)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /notifications", func(w http.ResponseWriter, r *http.Request) {
		if latencyMs > 0 {
			time.Sleep(time.Duration(latencyMs) * time.Millisecond)
		}
		roll := rand.Float64()
		switch {
		case roll < timeoutRate:
			// nunca responde: simula timeout no cliente.
			select {}
		case roll < timeoutRate+failureRate:
			status := []int{429, 500, 503}[rand.Intn(3)]
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "falha simulada"})
		default:
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"provider_message_id": randomID(),
			})
		}
	})

	logger.Info("dependency-simulator ouvindo", "addr", addr, "failure_rate", failureRate, "timeout_rate", timeoutRate, "latency_ms", latencyMs)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("servidor encerrou", "error", err)
		os.Exit(1)
	}
}

func randomID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 16)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}
