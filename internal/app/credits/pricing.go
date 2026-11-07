package credits

import (
	"math"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/models"
)

// Usage is what one charge used: AI tokens on a model, or a count of a priced
// unit (a web search, a placement test).
type Usage struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Unit         string
	Quantity     int
}

// AIUsage is the tokens one AI call used.
func AIUsage(model string, inputTokens, outputTokens int) Usage {
	return Usage{Model: model, InputTokens: inputTokens, OutputTokens: outputTokens}
}

// UnitUsage is a count of a priced unit.
func UnitUsage(unit string, quantity int) Usage {
	return Usage{Unit: unit, Quantity: quantity}
}

// Price is what a usage costs: the provider's price (cost) and what the
// balance is charged for it (cost plus the markup). Both round up to the next
// micro-dollar, so a call that used anything is never free.
func Price(p instancesettings.Usage, u Usage) (charge, cost models.Micro) {
	var raw float64
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		// A price per million tokens is micro-dollars per token.
		m := p.ModelPrice(u.Model)
		raw += float64(max(u.InputTokens, 0))*m.InputPerMTok + float64(max(u.OutputTokens, 0))*m.OutputPerMTok
	}
	if u.Unit != "" && u.Quantity > 0 {
		raw += float64(u.Quantity) * p.Units[u.Unit] * float64(models.MicroPerUSD)
	}
	if raw <= 0 {
		return 0, 0
	}
	cost = models.Micro(math.Ceil(raw))
	charge = models.Micro(math.Ceil(raw * float64(100+p.Markup()) / 100))
	return charge, cost
}
