package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func IsUniqueConstraintViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505"
}

func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23503"
}

// AsPgError returns the underlying *pgconn.PgError, or nil when err is not a
// Postgres error. Callers use it to inspect constraint names / codes.
func AsPgError(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) {
		return nil
	}

	return pgErr
}
