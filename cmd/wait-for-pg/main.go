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
	dsn := flag.String("dsn", "postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable", "PostgreSQL DSN")
	timeout := flag.Duration("timeout", 30*time.Second, "max time to wait")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)

	ticker := time.NewTicker(time.Second)

	fmt.Fprintf(os.Stderr, "Waiting for PostgreSQL at %s (timeout %s)…\n", *dsn, *timeout)

	for {
		conn, err := pgx.Connect(ctx, *dsn)
		if err == nil {
			ticker.Stop()
			cancel()
			_ = conn.Close(ctx)
			fmt.Fprintln(os.Stderr, "PostgreSQL is ready.")
			return
		}
		fmt.Fprintf(os.Stderr, "  not ready: %v\n", err)

		select {
		case <-ctx.Done():
			ticker.Stop()
			cancel()
			fmt.Fprintf(os.Stderr, "Timed out waiting for PostgreSQL: %v\n", ctx.Err())
			os.Exit(1)
		case <-ticker.C:
		}
	}
}
