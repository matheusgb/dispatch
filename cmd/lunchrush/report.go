package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type summary struct {
	Seed               int64         `json:"seed"`
	Orders             int           `json:"orders"`
	Couriers           int           `json:"couriers"`
	Completed          int           `json:"completed"`
	Declined           int           `json:"declined"`
	Expired            int           `json:"expired"`
	Errors             int           `json:"errors"`
	DuplicatesChecked  int           `json:"duplicates_checked"`
	DuplicatesOK       int           `json:"duplicates_ok"`
	TotalAssignRetries int           `json:"total_assign_retries"`
	Duration           time.Duration `json:"duration_ns"`
	Results            []orderResult `json:"results"`
	FailureSamples     []orderResult `json:"failure_samples,omitempty"`
}

func summarize(seed int64, courierCount int, started time.Time, results []orderResult) summary {
	s := summary{Seed: seed, Orders: len(results), Couriers: courierCount, Duration: time.Since(started), Results: results}
	for _, r := range results {
		switch {
		case r.Err != "":
			s.Errors++
			if len(s.FailureSamples) < 10 {
				s.FailureSamples = append(s.FailureSamples, r)
			}
		case r.Outcome == outcomeCompleted:
			s.Completed++
		case r.Outcome == outcomeDeclined:
			s.Declined++
		case r.Outcome == outcomeExpired:
			s.Expired++
		}
		if r.DuplicateChecked {
			s.DuplicatesChecked++
			if r.DuplicateOK {
				s.DuplicatesOK++
			}
		}
		s.TotalAssignRetries += r.AssignRetries
	}
	return s
}

func writeReport(prefix string, s summary) error {
	jsonPath := prefix + ".json"
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		return err
	}

	mdPath := prefix + ".md"
	md := fmt.Sprintf(`# Relatório LunchRush

- seed: %d
- ordens simuladas: %d
- entregadores no pool: %d
- duração total: %s

## Resultado por desfecho

| Desfecho | Quantidade |
| --- | --- |
| concluídas (assign -> pickup -> deliver) | %d |
| recusadas | %d |
| expiradas | %d |
| erros | %d |

## Idempotência

- chaves repetidas testadas: %d
- repetições que devolveram o mesmo ID: %d

## Disputa por entregador

- total de tentativas de atribuição rejeitadas por entregador ocupado, absorvidas por retry no pool: %d

`, s.Seed, s.Orders, s.Couriers, s.Duration, s.Completed, s.Declined, s.Expired, s.Errors,
		s.DuplicatesChecked, s.DuplicatesOK, s.TotalAssignRetries)

	if len(s.FailureSamples) > 0 {
		md += "## Amostra de falhas\n\n"
		for _, f := range s.FailureSamples {
			md += fmt.Sprintf("- ordem %d (entrega %s): %s\n", f.Index, f.DeliveryID, f.Err)
		}
	}

	return os.WriteFile(mdPath, []byte(md), 0o644)
}
