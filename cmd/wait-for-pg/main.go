// Command wait-for-pg polls a PostgreSQL server until it accepts connections.
//
// Usage:
//
//	go run ./cmd/wait-for-pg -dsn "postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable" -timeout 30s
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ralscha/faker-pg/internal/pgutil"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, output io.Writer) int {
	flags := flag.NewFlagSet("wait-for-pg", flag.ContinueOnError)
	flags.SetOutput(output)
	dsn := flags.String("dsn", "postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable", "PostgreSQL DSN")
	timeout := flags.Duration("timeout", 30*time.Second, "max time to wait")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 || *timeout <= 0 {
		_, _ = fmt.Fprintln(output, "Use -dsn for the connection string and a positive -timeout duration.")
		return 1
	}
	cfg, err := pgx.ParseConfig(*dsn)
	if err != nil {
		_, _ = fmt.Fprintln(output, pgutil.RedactParseError(err))
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	_, _ = fmt.Fprintf(output, "Waiting for PostgreSQL at %s (timeout %s)...\n", connectionLabel(*dsn), *timeout)

	for {
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err == nil {
			_ = conn.Close(context.Background())
			_, _ = fmt.Fprintln(output, "PostgreSQL is ready.")
			return 0
		}
		_, _ = fmt.Fprintf(output, "  not ready: %v\n", err)

		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintf(output, "Timed out waiting for PostgreSQL: %v\n", ctx.Err())
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
	return fmt.Sprintf("%s/%s", net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), cfg.Database)
}
