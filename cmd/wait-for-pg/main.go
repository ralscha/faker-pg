// Command wait-for-pg polls a PostgreSQL server until it accepts connections.
//
// Usage:
//
//	go run ./cmd/wait-for-pg -dsn "postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable" -timeout 30s
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	os.Exit(run())
}

func run() int {
	dsn := flag.String("dsn", "postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable", "PostgreSQL DSN")
	timeout := flag.Duration("timeout", 30*time.Second, "max time to wait")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	fmt.Fprintf(os.Stderr, "Waiting for PostgreSQL at %s (timeout %s)...\n", connectionLabel(*dsn), *timeout)

	for {
		conn, err := pgx.Connect(ctx, *dsn)
		if err == nil {
			_ = conn.Close(context.Background())
			fmt.Fprintln(os.Stderr, "PostgreSQL is ready.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "  not ready: %v\n", err)

		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "Timed out waiting for PostgreSQL: %v\n", ctx.Err())
			return 1
		case <-ticker.C:
		}
	}
}

func connectionLabel(dsn string) string {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return "the configured server"
	}
	return fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
}
