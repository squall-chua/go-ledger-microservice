package repository

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryableRecognisesDeadlockAndSerializationFailure(t *testing.T) {
	for code, want := range map[string]bool{"40P01": true, "40001": true, "23505": false} {
		if got := IsRetryable(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code})); got != want {
			t.Errorf("IsRetryable(SQLSTATE %s) = %v, want %v", code, got, want)
		}
	}
	if IsRetryable(errors.New("not a database error")) {
		t.Error("a plain error is not retryable")
	}
}
