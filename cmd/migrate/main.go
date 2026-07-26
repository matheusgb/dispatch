// Comando migrate aplica ou desfaz as migrations de migrations/ contra
// DATABASE_URL. Não existe rollback automático em produção: este binário é
// ferramenta de operador, chamado explicitamente.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: migrate <up|down> [DATABASE_URL]")
		os.Exit(2)
	}
	direction := os.Args[1]

	dsn := os.Getenv("DATABASE_URL")
	if len(os.Args) > 2 {
		dsn = os.Args[2]
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL não definido")
		os.Exit(2)
	}

	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "abrir migrate:", err)
		os.Exit(1)
	}

	switch direction {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	default:
		fmt.Fprintln(os.Stderr, "direção desconhecida:", direction)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	fmt.Println("ok")
}
