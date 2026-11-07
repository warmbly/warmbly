package models

import "math"

// Micro is an amount of US dollars in millionths. Balances, charges and limits
// are held in it so a short AI call that costs a fraction of a cent is exact.
type Micro int64

// MicroPerUSD is one dollar in Micro.
const MicroPerUSD Micro = 1_000_000

// USDToMicro converts a dollar amount, rounded to the nearest micro-dollar.
func USDToMicro(usd float64) Micro {
	return Micro(math.Round(usd * float64(MicroPerUSD)))
}

// USD is the amount in dollars, for JSON and display.
func (m Micro) USD() float64 {
	return float64(m) / float64(MicroPerUSD)
}

// Cents is the amount in whole cents, rounded to the nearest cent.
func (m Micro) Cents() int64 {
	return int64(math.Round(float64(m) / 10_000))
}

// MicroFromCents converts a Stripe minor-unit USD amount.
func MicroFromCents(cents int64) Micro {
	return Micro(cents * 10_000)
}

// LegacyCreditMicro is what one pre-dollar credit is worth; the migration's
// legacy_credit_micro() holds the same value.
const LegacyCreditMicro Micro = 20_000

// CreditsFromMicro is the legacy credit figure older clients still read.
func CreditsFromMicro(m Micro) int {
	if m <= 0 {
		return 0
	}
	return int(m / LegacyCreditMicro)
}
