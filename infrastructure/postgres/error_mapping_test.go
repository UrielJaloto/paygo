package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

var errDomain = errors.New("domain error")

func TestIsUniqueViolation_Errors_MatchNamedConstraintOnly(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "named constraint", err: &pgconn.PgError{Code: "23505", ConstraintName: "named"}, want: true},
		{name: "wrapped", err: fmt.Errorf("saving: %w", &pgconn.PgError{Code: "23505", ConstraintName: "named"}), want: true},
		{name: "other constraint", err: &pgconn.PgError{Code: "23505", ConstraintName: "other"}},
		{name: "other code", err: &pgconn.PgError{Code: "23503", ConstraintName: "named"}},
		{name: "not a database error", err: errDomain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := postgres.IsUniqueViolation(test.err, "named"); got != test.want {
				t.Fatalf("IsUniqueViolation() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNotFound_Errors_MapOnlyMissingRows(t *testing.T) {
	other := errors.New("connection lost")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "no rows", err: pgx.ErrNoRows, want: errDomain},
		{name: "other error", err: other, want: other},
		{name: "no error", err: nil, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := postgres.NotFound(test.err, errDomain); !errors.Is(got, test.want) {
				t.Fatalf("NotFound() = %v, want %v", got, test.want)
			}
		})
	}
}
