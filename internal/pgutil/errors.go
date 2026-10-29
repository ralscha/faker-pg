// Package pgutil provides shared PostgreSQL connection helpers.
package pgutil

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// RedactParseError hides malformed connection strings, which may contain
// credentials that the driver's best-effort password redaction cannot detect.
// The original error remains available through errors.Is and errors.As.
func RedactParseError(err error) error {
	if _, ok := errors.AsType[*pgconn.ParseConfigError](err); ok {
		return redactedParseError{err: err}
	}
	return err
}

type redactedParseError struct {
	err error
}

func (e redactedParseError) Error() string {
	return "invalid PostgreSQL connection settings; check the DSN, environment variables, and service file"
}

func (e redactedParseError) Unwrap() error {
	return e.err
}
