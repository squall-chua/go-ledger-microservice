package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"time"
)

// fingerprintOf is the deterministic hash of a transaction's financial content,
// and is what tells an idempotency replay (same key, same content) from a key
// reused with different content.
//
// What goes in: the note, the supplied date, the metadata pairs sorted by key,
// and the postings in request order. Reordering the postings is a different
// transaction and hashes differently.
//
// What stays out: a stamped date, because the ledger picks a different instant
// for every identical retry of a caller who supplies none, and the accounts
// named for non-negative verification, because they are a precondition on the
// write rather than content of it.
func fingerprintOf(draft TransactionDraft) string {
	sum := sha256.New()
	// Every part is written length-prefixed, so no value can be mistaken for a
	// delimiter. Each count is terminated with a semicolon for the same reason:
	// without it the count's digits run straight into the following length
	// prefix, the two decimal runs merge, and the pre-image can be re-cut into a
	// different draft that hashes the same.
	field := func(parts ...string) {
		for _, part := range parts {
			fmt.Fprintf(sum, "%d:%s", len(part), part)
		}
	}

	field("note", draft.Note)
	if draft.Date != nil {
		field("date", draft.Date.UTC().Format(time.RFC3339Nano))
	}

	fmt.Fprintf(sum, "metadata=%d;", len(draft.Metadata))
	for _, key := range slices.Sorted(maps.Keys(draft.Metadata)) {
		field(key, draft.Metadata[key])
	}

	fmt.Fprintf(sum, "postings=%d;", len(draft.Postings))
	for _, posting := range draft.Postings {
		field(posting.Account.Type, posting.Account.User, posting.Account.Name,
			posting.CurrencyCode, posting.Amount.String())
	}

	return hex.EncodeToString(sum.Sum(nil))
}
