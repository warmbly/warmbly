package store

import (
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/models"
)

const microsPerCent = 10_000

// ceilCent rounds a positive micro amount up to a whole cent.
func ceilCent(m int64) int64 {
	if m <= 0 {
		return 0
	}
	return (m + microsPerCent - 1) / microsPerCent * microsPerCent
}

// retailDomain is wholesale × (1 + pct/100) + fixed, rounded up to the cent.
func retailDomain(r instancesettings.StoreRetail, wholesale int64) int64 {
	if wholesale <= 0 {
		return 0
	}
	marked := (wholesale*int64(100+r.MarkupPercent()) + 99) / 100
	return ceilCent(marked + r.MarkupFixed())
}

// atCost reports retail domain prices equal to wholesale.
func atCost(r instancesettings.StoreRetail) bool {
	return r.MarkupPercent() == 0 && r.MarkupFixed() == 0
}

// plan splits what is due between the balance and the card. When the balance
// falls short, the card is charged the shortfall rounded up to the cent, and
// never less than floor (the store's minimum, or the automatic reload amount),
// so card charges stay few and large; what is left over stays on the balance.
// No card is charged when the balance covers it.
func plan(due, balance int64, card bool, floor int64) Payment {
	p := Payment{BalanceMicros: max(balance, 0)}
	p.FromBalanceMicros = min(p.BalanceMicros, due)
	short := due - p.FromBalanceMicros
	if short > 0 {
		p.CardChargeMicros = max(ceilCent(short), floor, instancesettings.StoreMinCardFloor)
		p.NeedsPaymentMethod = !card
	}
	p.BalanceAfterMicros = p.BalanceMicros + p.CardChargeMicros - due
	return p
}

func micro(v int64) models.Micro { return models.Micro(v) }
