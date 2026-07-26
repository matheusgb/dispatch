// Comando cellrouter é o roteador de célula do tier 5: recebe o cabeçalho
// X-Cell-ID e encaminha a requisição para o delivery-api daquela célula,
// sem nunca consultar todas as células para descobrir onde uma entrega
// mora (o roadmap é explícito sobre isso). O diretório de células é
// estático nesta implementação de referência (um mapa cell_id -> backend
// carregado de variável de ambiente), porque o roadmap trata o "diretório
// global" como um problema de descoberta separado (DynamoDB Global Tables
// no desenho de referência), não como parte do protocolo de fencing em si
// — ver docs/adr/0019-arquitetura-celular-local.md.
//
// Isto não é um proxy de propósito geral: ele não tenta ser resiliente a
// nenhuma falha do backend além de propagar o erro. Um cell router de
// produção teria timeout por célula, circuit breaker e métricas de
// roteamento por cell_id; aqui o objetivo é provar o roteamento sem
// full-scan, não construir um proxy maduro.
package main

import (
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

const defaultCellHeader = "X-Cell-ID"

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	defaultCell := os.Getenv("DEFAULT_CELL_ID")

	// CELLS tem o formato "cell-a=http://delivery-api-cell-a:8080,cell-b=http://delivery-api-cell-b:8080"
	cellsEnv := os.Getenv("CELLS")
	if cellsEnv == "" {
		log.Fatal("CELLS não definido: formato \"cell-a=http://host:porta,cell-b=http://host:porta\"")
	}
	backends, err := parseCells(cellsEnv)
	if err != nil {
		log.Fatalf("CELLS inválido: %v", err)
	}
	if defaultCell == "" {
		for cellID := range backends {
			defaultCell = cellID
			break
		}
	}

	proxies := make(map[string]*httputil.ReverseProxy, len(backends))
	for cellID, target := range backends {
		proxies[cellID] = httputil.NewSingleHostReverseProxy(target)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cellID := r.Header.Get(defaultCellHeader)
		if cellID == "" {
			cellID = defaultCell
		}
		proxy, ok := proxies[cellID]
		if !ok {
			logger.Warn("célula desconhecida", "cell_id", cellID, "path", r.URL.Path)
			http.Error(w, `{"error":"célula desconhecida"}`, http.StatusBadGateway)
			return
		}
		logger.Info("roteando", "cell_id", cellID, "path", r.URL.Path, "method", r.Method)
		proxy.ServeHTTP(w, r)
	})

	log.Printf("cellrouter ouvindo em %s, células: %v, default=%s", addr, keys(backends), defaultCell)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func parseCells(spec string) (map[string]*url.URL, error) {
	out := make(map[string]*url.URL)
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		u, err := url.Parse(parts[1])
		if err != nil {
			return nil, err
		}
		out[parts[0]] = u
	}
	return out, nil
}

func keys(m map[string]*url.URL) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
