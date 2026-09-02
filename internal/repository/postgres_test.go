package repository

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
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

// The listings themselves are exercised through real RPCs against a real
// database in internal/service. Only the page bounds are pinned here: they are
// a pure function, and the maximum is not worth a hundred and one round trips.
func TestPageBounds(t *testing.T) {
	cases := map[string]struct {
		page   Page
		limit  int32
		offset int32
	}{
		"an unasked-for size defaults to ten": {Page{}, 10, 0},
		"a negative size defaults to ten":     {Page{Size: -5}, 10, 0},
		"a size over the maximum is clamped":  {Page{Size: 1000}, 100, 0},
		"the maximum itself is kept":          {Page{Size: 100}, 100, 0},
		"page numbers are one-based":          {Page{Size: 20, Number: 3}, 20, 40},
		"a page below one is the first page":  {Page{Size: 20, Number: 0}, 20, 0},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			limit, offset := testCase.page.bounds()
			assert.Equal(t, testCase.limit, limit)
			assert.Equal(t, testCase.offset, offset)
		})
	}
}
