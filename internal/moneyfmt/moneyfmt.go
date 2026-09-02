package moneyfmt

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
	"google.golang.org/genproto/googleapis/type/money"
)

// maxNanos is the largest magnitude the nanos field may carry; anything beyond
// it belongs in units.
const maxNanos = 999999999

// ToDecimal converts google.type.Money to shopspring/decimal.Decimal, refusing a
// malformed amount: an empty currency code, out-of-range nanos, or units and
// nanos disagreeing on sign.
func ToDecimal(m *money.Money) (decimal.Decimal, error) {
	if m == nil {
		return decimal.Zero, errors.New("money is nil")
	}
	if m.CurrencyCode == "" {
		return decimal.Zero, errors.New("currency code is empty")
	}
	if m.Nanos < -maxNanos || m.Nanos > maxNanos {
		return decimal.Zero, fmt.Errorf("nanos %d is outside [-%d, %d]", m.Nanos, maxNanos, maxNanos)
	}
	if (m.Units > 0 && m.Nanos < 0) || (m.Units < 0 && m.Nanos > 0) {
		return decimal.Zero, fmt.Errorf("units %d and nanos %d have different signs", m.Units, m.Nanos)
	}
	// Nanos are exactly nine decimal places, which is the precision storage keeps.
	return decimal.NewFromInt(m.Units).Add(decimal.New(int64(m.Nanos), -9)), nil
}

// FromDecimal converts shopspring/decimal.Decimal to google.type.Money,
// refusing an amount whose integer part does not fit the int64 units field.
// Storage is NUMERIC(38,9) and a balance is a sum, so a stored amount can be
// larger than any single posting: wrapping it silently would put a different
// number on the wire from the one in the book.
func FromDecimal(d decimal.Decimal, currency string) (*money.Money, error) {
	integer := d.BigInt()
	if !integer.IsInt64() {
		return nil, fmt.Errorf("amount %s is outside the range money can carry", d)
	}
	units := integer.Int64()
	nanos := d.Sub(decimal.NewFromInt(units)).Mul(decimal.NewFromInt(1e9)).IntPart()

	return &money.Money{
		CurrencyCode: currency,
		Units:        units,
		Nanos:        int32(nanos),
	}, nil
}
