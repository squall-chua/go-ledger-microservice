package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0001 in full, without a database. The clock is a parameter, so every
// branch is reachable from a plain table.

var clock = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func at(offset time.Duration) *time.Time {
	moment := clock.Add(offset)
	return &moment
}

func TestResolveDateStampsWhenTheCallerSuppliesNone(t *testing.T) {
	cases := map[string]struct {
		latest time.Time
		want   time.Time
	}{
		"an account with no postings takes the clock": {
			latest: time.Time{},
			want:   clock,
		},
		"a date behind the clock is left alone": {
			latest: clock.Add(-time.Hour),
			want:   clock,
		},
		"a posting at the same instant is advanced past": {
			latest: clock,
			want:   clock.Add(time.Microsecond),
		},
		"a posting parked ahead of the clock is advanced past": {
			latest: clock.Add(time.Hour),
			want:   clock.Add(time.Hour + time.Microsecond),
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			date, err := resolveDate(nil, testCase.latest, clock)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, date)
		})
	}
}

// A caller who supplied no date is never told its date was wrong.
func TestEachStampedDateBehindAParkedPostingIsDistinct(t *testing.T) {
	latest := clock.Add(time.Hour)

	first, err := resolveDate(nil, latest, clock)
	require.NoError(t, err)
	second, err := resolveDate(nil, first, clock)
	require.NoError(t, err)

	assert.True(t, first.After(latest), "the first lands past the parked posting")
	assert.True(t, second.After(first), "the second lands past the first")
}

func TestResolveDatePolicesASuppliedDate(t *testing.T) {
	cases := map[string]struct {
		supplied *time.Time
		latest   time.Time
		want     time.Time
		wantErr  error
	}{
		"a date past the latest posting is kept": {
			supplied: at(-time.Hour),
			latest:   clock.Add(-2 * time.Hour),
			want:     clock.Add(-time.Hour),
		},
		"a date equal to the latest posting is kept": {
			supplied: at(-time.Hour),
			latest:   clock.Add(-time.Hour),
			want:     clock.Add(-time.Hour),
		},
		"the first posting to an account is never backdated": {
			supplied: at(-10 * 365 * 24 * time.Hour),
			latest:   time.Time{},
			want:     clock.Add(-10 * 365 * 24 * time.Hour),
		},
		"a date before the latest posting is refused": {
			supplied: at(-2 * time.Hour),
			latest:   clock.Add(-time.Hour),
			wantErr:  ErrBackdated,
		},
		"a date one microsecond before it is refused": {
			supplied: at(-time.Hour - time.Microsecond),
			latest:   clock.Add(-time.Hour),
			wantErr:  ErrBackdated,
		},
		"a drifting clock inside the tolerance is kept": {
			supplied: at(4 * time.Minute),
			want:     clock.Add(4 * time.Minute),
		},
		"the tolerance itself is kept": {
			supplied: at(futureTolerance),
			want:     clock.Add(futureTolerance),
		},
		"a date past the tolerance is refused": {
			supplied: at(futureTolerance + time.Microsecond),
			wantErr:  ErrDateTooFarAhead,
		},
		// A caller sending both faults is told about the one it can fix.
		"the future cap is judged before the backdating guard": {
			supplied: at(time.Hour),
			latest:   clock.Add(2 * time.Hour),
			wantErr:  ErrDateTooFarAhead,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			date, err := resolveDate(testCase.supplied, testCase.latest, clock)
			if testCase.wantErr != nil {
				assert.ErrorIs(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, date)
		})
	}
}

// Storage resolves to the microsecond, so a date is cut down to it before
// anything else judges it — otherwise a supplied date and a stamped one land at
// different resolutions and the fingerprint covers digits that never landed.
func TestDatesAreCutDownToTheMicrosecond(t *testing.T) {
	odd := clock.Add(1500 * time.Nanosecond)
	even := clock.Add(time.Microsecond)

	assert.Equal(t, even, truncateDate(odd))
	assert.Equal(t, even, provisionalDate(nil, odd))
	assert.Equal(t, even, provisionalDate(&odd, clock))

	date, err := resolveDate(&odd, time.Time{}, clock)
	require.NoError(t, err)
	assert.Equal(t, even, date)

	// A supplied date carrying nanoseconds is not advanced past a latest that
	// only looks earlier because those nanoseconds were dropped.
	_, err = resolveDate(&odd, even, clock)
	assert.NoError(t, err)
}

// A date arriving in another zone is stored as the same instant in UTC, so the
// fingerprint of a retry sending it differently does not change.
func TestADateIsNormalisedToUTC(t *testing.T) {
	elsewhere := clock.In(time.FixedZone("UTC+8", 8*60*60))

	date, err := resolveDate(&elsewhere, time.Time{}, clock)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, date.Location())
	assert.True(t, date.Equal(clock))
}
