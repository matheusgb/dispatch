// Sem build tag de propósito: usado tanto pelos contract tests que exigem
// Postgres (build tag integration) quanto pelo teste estático de tracking
// (sem build tag, roda em qualquer `go test ./...`).
package contract

import (
	"os"
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// test/contract -> raiz do repo
	return filepath.Join(wd, "..", "..")
}
