package pgutil

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRedactParseError(t *testing.T) {
	for _, dsn := range []string{
		"postgres://user:super-secret@localhost:bad/db",
		"host=localhost password='super-secret",
		"postgres://user:super-secret%ZZ@localhost/db",
	} {
		_, err := pgx.ParseConfig(dsn)
		if err == nil {
			t.Fatal("expected invalid test DSN")
		}
		got := RedactParseError(fmt.Errorf("connect: %w", err))
		if strings.Contains(got.Error(), "super-secret") || strings.Contains(got.Error(), dsn) {
			t.Fatal("parse error exposed connection credentials")
		}
		var original *pgconn.ParseConfigError
		if !errors.As(got, &original) || !errors.Is(got, err) {
			t.Fatal("original error was lost")
		}
	}
	err := errors.New("connection refused")
	if !errors.Is(RedactParseError(err), err) || RedactParseError(nil) != nil {
		t.Fatal("non-parse errors should be unchanged")
	}
}
