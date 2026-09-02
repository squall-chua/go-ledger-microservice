package repository

import (
	"errors"
	"time"
)

// The transaction-date rule of ADR-0001, in one place. A transaction date is
// either supplied by the caller — a claim about when the event happened, which
// is policed — or stamped by the ledger when the caller omits one, which is the
// transaction's position in the affected accounts' order rather than a claim,
// and so is advanced rather than refused.
//
// The clock is a parameter, so the whole rule is exercised without a database.

// futureTolerance is how far ahead of the ledger's clock a supplied date may
// sit. Without a cap one bad client clock parks a posting far in the future and
// every later write to that account is refused as backdated — unrecoverable in
// an append-only ledger. With it, a caller whose clock merely drifts still gets
// the date it sent.
const futureTolerance = 5 * time.Minute

// ErrDateTooFarAhead reports a supplied date more than futureTolerance past the
// ledger's clock. The caller sent a date it should not have, so the service
// reports it as InvalidArgument rather than FailedPrecondition.
var ErrDateTooFarAhead = errors.New("date is more than five minutes in the future")

// truncateDate cuts a date down to the microsecond a timestamptz column
// resolves to. Every date the ledger handles passes through here, and that is
// what makes the rest consistent: the date returned to the caller is the date
// the listings read back, "advance past latest" is judged on the value that
// actually lands rather than on digits storage drops, and the fingerprint
// covers the date as recorded rather than one that was never stored.
func truncateDate(date time.Time) time.Time {
	return date.UTC().Truncate(time.Microsecond)
}

// provisionalDate is the transaction date before the account locks are taken,
// while what it has to clear is still unknown. The balance rows are seeded with
// it — seeding a new account with this transaction's own date is what stops the
// backdating guard from refusing the first posting to that account — and
// resolveDate starts from it once `latest` has been read back.
func provisionalDate(supplied *time.Time, now time.Time) time.Time {
	if supplied != nil {
		return truncateDate(*supplied)
	}
	return truncateDate(now)
}

// resolveDate is the transaction date to store. `latest` is the latest posting
// date of any account the transaction touches, read under the row locks so no
// concurrent writer can move it afterwards.
func resolveDate(supplied *time.Time, latest, now time.Time) (time.Time, error) {
	date := provisionalDate(supplied, now)

	if supplied == nil {
		// A stamped date lands strictly past `latest`, by the microsecond the
		// column resolves to, so a run of stamped transactions behind one
		// posting parked ahead of the clock gets a date each rather than
		// collapsing onto that posting's date. A caller who supplied no date is
		// never told its date was wrong, and this absorbs clock skew between
		// ledger instances.
		if !date.After(latest) {
			date = latest.Add(time.Microsecond)
		}
		return date, nil
	}

	// A supplied date is the caller's own claim, so it is refused rather than
	// moved. The future cap is judged first: a date the caller should never
	// have sent is the caller's mistake, while a backdated one is a fact about
	// the book that only the locks above could reveal.
	if date.After(now.Add(futureTolerance)) {
		return time.Time{}, ErrDateTooFarAhead
	}
	if date.Before(latest) {
		return time.Time{}, ErrBackdated
	}
	return date, nil
}
