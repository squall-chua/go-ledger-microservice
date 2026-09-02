package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFingerprintCoversTheFinancialContentAndNothingElse(t *testing.T) {
	supplied := time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)
	base := func() TransactionDraft {
		return TransactionDraft{
			IdempotencyKey: "key",
			Date:           &supplied,
			Note:           "a transfer",
			Metadata:       map[string]string{"order": "42", "source": "checkout"},
			Postings: []PostingDraft{
				{Account: Account{"ASSETS", "alice", "Checking"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(100)},
				{Account: Account{"EQUITIES", "", "Opening"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(-100)},
			},
			VerifyNonNegative: []Account{{"ASSETS", "alice", "Checking"}},
		}
	}

	// Content the fingerprint has to see: two requests differing in any of it
	// are different transactions and must not replay each other.
	content := map[string]func(*TransactionDraft){
		"a different note":      func(d *TransactionDraft) { d.Note = "another transfer" },
		"a different date":      func(d *TransactionDraft) { later := supplied.Add(time.Hour); d.Date = &later },
		"an omitted date":       func(d *TransactionDraft) { d.Date = nil },
		"a different amount":    func(d *TransactionDraft) { d.Postings[0].Amount = decimal.NewFromInt(101) },
		"a different account":   func(d *TransactionDraft) { d.Postings[0].Account.Name = "Savings" },
		"a different currency":  func(d *TransactionDraft) { d.Postings[0].CurrencyCode = "EUR" },
		"a different metadata":  func(d *TransactionDraft) { d.Metadata["order"] = "43" },
		"an extra metadata key": func(d *TransactionDraft) { d.Metadata["extra"] = "" },
		"one fewer metadata":    func(d *TransactionDraft) { delete(d.Metadata, "order") },
		"reordered postings": func(d *TransactionDraft) {
			d.Postings[0], d.Postings[1] = d.Postings[1], d.Postings[0]
		},
	}

	// Everything else is either the key itself or a precondition on the write,
	// and two requests differing only in it still replay.
	incidental := map[string]func(*TransactionDraft){
		"a different idempotency key": func(d *TransactionDraft) { d.IdempotencyKey = "another-key" },
		"different accounts to verify": func(d *TransactionDraft) {
			d.VerifyNonNegative = []Account{{"EQUITIES", "", "Opening"}}
		},
		"no accounts to verify": func(d *TransactionDraft) { d.VerifyNonNegative = nil },
	}

	want := fingerprintOf(base())
	assert.Equal(t, want, fingerprintOf(base()), "identical content hashes identically")

	for name, change := range content {
		t.Run(name, func(t *testing.T) {
			draft := base()
			change(&draft)
			assert.NotEqual(t, want, fingerprintOf(draft))
		})
	}

	for name, change := range incidental {
		t.Run(name, func(t *testing.T) {
			draft := base()
			change(&draft)
			assert.Equal(t, want, fingerprintOf(draft))
		})
	}
}

// A stamped date is not content: the ledger picks a different instant for every
// identical retry of a caller who supplies none, so it cannot be hashed in.
func TestFingerprintIgnoresAStampedDate(t *testing.T) {
	draft := TransactionDraft{
		Note: "no date at all",
		Postings: []PostingDraft{
			{Account: Account{"ASSETS", "alice", "Checking"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(1)},
			{Account: Account{"EQUITIES", "", "Opening"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(-1)},
		},
	}

	assert.Equal(t, fingerprintOf(draft), fingerprintOf(draft),
		"two retries that omit the date hash the same however long apart they are")

	// And an absent date is not the same as any supplied one.
	stamped := time.Now().UTC()
	dated := draft
	dated.Date = &stamped
	assert.NotEqual(t, fingerprintOf(draft), fingerprintOf(dated))
}

// A metadata key and its value are hashed as separate length-prefixed parts, so
// no pair can be re-cut into a different one that hashes the same.
func TestFingerprintDoesNotConfuseMetadataKeysWithValues(t *testing.T) {
	postings := []PostingDraft{
		{Account: Account{"ASSETS", "alice", "Checking"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(1)},
		{Account: Account{"EQUITIES", "", "Opening"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(-1)},
	}

	assert.NotEqual(t,
		fingerprintOf(TransactionDraft{Note: "note", Metadata: map[string]string{"a": "bc"}, Postings: postings}),
		fingerprintOf(TransactionDraft{Note: "note", Metadata: map[string]string{"ab": "c"}, Postings: postings}))
}

// A decimal is hashed in its normalised form, so the same amount expressed two
// ways is the same content.
func TestFingerprintNormalisesAmounts(t *testing.T) {
	draft := func(amount decimal.Decimal) TransactionDraft {
		return TransactionDraft{
			Note: "note",
			Postings: []PostingDraft{
				{Account: Account{"ASSETS", "alice", "Checking"}, CurrencyCode: "USD", Amount: amount},
			},
		}
	}

	assert.Equal(t, fingerprintOf(draft(decimal.NewFromInt(50))), fingerprintOf(draft(decimal.New(5000, -2))))
}

// The pair count used to be written without a terminator, so its digits ran
// straight into the following length prefix. That let one draft's pre-image be
// re-cut into a different draft's: a single metadata pair whose key began "0:2:"
// hashed the same as eleven pairs. A caller retrying under a colliding key got
// somebody else's transaction back and its own money was never recorded.
func TestFingerprintDoesNotCollideOnCountBoundaries(t *testing.T) {
	postings := []PostingDraft{
		{Account: Account{"ASSETS", "alice", "Checking"}, CurrencyCode: "USD", Amount: decimal.NewFromInt(1)},
	}
	draft := func(metadata map[string]string) TransactionDraft {
		return TransactionDraft{Note: "note", Metadata: metadata, Postings: postings}
	}

	filler := strings.Repeat("F", 46)
	one := draft(map[string]string{
		"aa0:2:ab50:x": filler + "1:b0:1:c0:1:d0:1:e0:1:f0:1:g0:1:h0:1:i0:1:j0:",
	})
	eleven := draft(map[string]string{
		"aa": "", "ab": "x91:" + filler,
		"b": "", "c": "", "d": "", "e": "", "f": "", "g": "", "h": "", "i": "", "j": "",
	})

	require.Len(t, one.Metadata, 1)
	require.Len(t, eleven.Metadata, 11)
	assert.NotEqual(t, fingerprintOf(one), fingerprintOf(eleven),
		"a count must not merge into the length prefix that follows it")
}
