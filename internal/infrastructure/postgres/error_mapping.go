package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const uniqueViolation = "23505"

func isUniqueViolation(err error, constraint string) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	return postgresError.Code == uniqueViolation && postgresError.ConstraintName == constraint
}

func mapNotFound(err error, domainErr error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainErr
	}
	return err
}
