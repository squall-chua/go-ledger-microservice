package moneyfmt

import (
	"errors"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"google.golang.org/genproto/googleapis/type/money"
)

// ToDecimal converts google.type.Money to shopspring/decimal.Decimal.
func ToDecimal(m *money.Money) (decimal.Decimal, error) {
	if m == nil {
		return decimal.Zero, errors.New("money is nil")
	}
	units := decimal.NewFromInt(m.Units)
	nanos := decimal.NewFromInt32(m.Nanos).Div(decimal.NewFromInt(1e9))
	return units.Add(nanos), nil
}

// FromDecimal converts shopspring/decimal.Decimal to google.type.Money.
func FromDecimal(d decimal.Decimal, currency string) *money.Money {
	units := d.IntPart()
	nanos := d.Sub(decimal.NewFromInt(units)).Mul(decimal.NewFromInt(1e9)).IntPart()

	return &money.Money{
		CurrencyCode: currency,
		Units:        units,
		Nanos:        int32(nanos),
	}
}

// ToDecimal128 converts decimal to MongoDB Decimal128.
func ToDecimal128(d decimal.Decimal) (bson.Decimal128, error) {
	return bson.ParseDecimal128(d.String())
}

// FromDecimal128 converts MongoDB Decimal128 to decimal.
func FromDecimal128(d bson.Decimal128) (decimal.Decimal, error) {
	return decimal.NewFromString(d.String())
}
