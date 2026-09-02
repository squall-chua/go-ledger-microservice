package moneyfmt

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genproto/googleapis/type/money"
)

func TestToDecimal(t *testing.T) {
	tests := []struct {
		name          string
		money         *money.Money
		expected      decimal.Decimal
		expectedError bool
	}{
		{
			name:          "Nil money",
			money:         nil,
			expected:      decimal.Zero,
			expectedError: true,
		},
		{
			name: "Positive amount with units only",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        100,
				Nanos:        0,
			},
			expected:      decimal.RequireFromString("100"),
			expectedError: false,
		},
		{
			name: "Positive amount with units and nanos",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        10,
				Nanos:        500000000,
			},
			expected:      decimal.RequireFromString("10.5"),
			expectedError: false,
		},
		{
			name: "Negative amount with units and nanos",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        -10,
				Nanos:        -500000000,
			},
			expected:      decimal.RequireFromString("-10.5"),
			expectedError: false,
		},
		{
			name: "Positive amount with nanos only",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        0,
				Nanos:        990000000,
			},
			expected:      decimal.RequireFromString("0.99"),
			expectedError: false,
		},
		{
			name: "Negative amount with nanos only",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        0,
				Nanos:        -990000000,
			},
			expected:      decimal.RequireFromString("-0.99"),
			expectedError: false,
		},
		{
			name: "Empty currency code",
			money: &money.Money{
				CurrencyCode: "",
				Units:        1,
				Nanos:        0,
			},
			expectedError: true,
		},
		{
			name: "Nanos above range",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        1,
				Nanos:        1000000000,
			},
			expectedError: true,
		},
		{
			name: "Nanos below range",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        -1,
				Nanos:        -1000000000,
			},
			expectedError: true,
		},
		{
			name: "Units and nanos disagree on sign",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        1,
				Nanos:        -500000000,
			},
			expectedError: true,
		},
		{
			name: "Nine digit precision",
			money: &money.Money{
				CurrencyCode: "USD",
				Units:        1,
				Nanos:        123456789,
			},
			expected:      decimal.RequireFromString("1.123456789"),
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToDecimal(tt.money)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.True(t, tt.expected.Equal(result))
			}
		})
	}
}

func TestFromDecimal(t *testing.T) {
	tests := []struct {
		name     string
		decimal  decimal.Decimal
		currency string
		expected *money.Money
	}{
		{
			name:     "Positive amount with units only",
			decimal:  decimal.RequireFromString("100"),
			currency: "USD",
			expected: &money.Money{
				CurrencyCode: "USD",
				Units:        100,
				Nanos:        0,
			},
		},
		{
			name:     "Positive amount with units and nanos",
			decimal:  decimal.RequireFromString("10.5"),
			currency: "EUR",
			expected: &money.Money{
				CurrencyCode: "EUR",
				Units:        10,
				Nanos:        500000000,
			},
		},
		{
			name:     "Negative amount with units and nanos",
			decimal:  decimal.RequireFromString("-10.5"),
			currency: "GBP",
			expected: &money.Money{
				CurrencyCode: "GBP",
				Units:        -10,
				Nanos:        -500000000,
			},
		},
		{
			name:     "Zero amount",
			decimal:  decimal.RequireFromString("0"),
			currency: "JPY",
			expected: &money.Money{
				CurrencyCode: "JPY",
				Units:        0,
				Nanos:        0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FromDecimal(tt.decimal, tt.currency)
			assert.Equal(t, tt.expected.CurrencyCode, result.CurrencyCode)
			assert.Equal(t, tt.expected.Units, result.Units)
			assert.Equal(t, tt.expected.Nanos, result.Nanos)
		})
	}
}

func TestRoundTripAtNineDigits(t *testing.T) {
	for _, in := range []*money.Money{
		{CurrencyCode: "USD", Units: 1, Nanos: 123456789},
		{CurrencyCode: "USD", Units: -1, Nanos: -123456789},
		{CurrencyCode: "USD", Units: 0, Nanos: 1},
		{CurrencyCode: "USD", Units: 9999999, Nanos: 999999999},
	} {
		d, err := ToDecimal(in)
		assert.NoError(t, err)
		out := FromDecimal(d, in.CurrencyCode)
		assert.Equal(t, in.Units, out.Units)
		assert.Equal(t, in.Nanos, out.Nanos)
	}
}
