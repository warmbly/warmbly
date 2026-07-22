package credits

import "strings"

// Every AI credit cost in the product, in one place. Handlers and services
// must reference these constants instead of hardcoding amounts, and the
// "AI credits" guide in docs/ mirrors this table.
const (
	// CostWritingAssistant is one writing-assistant generation (POST /generation/write).
	CostWritingAssistant = 1

	// CostAgentIteration is one iteration of the dashboard agent loop.
	CostAgentIteration = 1

	// CostReplyDraft is one context-grounded unibox reply draft.
	CostReplyDraft = 2

	// CostResearchRun is one contact research run (nothing_found included).
	CostResearchRun = 2

	// CostAutomationAINode is one single-shot AI step (classify/extract/generate
	// mode) or AI switch execution, and one Ask-AI branch evaluation.
	CostAutomationAINode = 1

	// CostCampaignAIStep is one campaign-sequence AI-decided "switch" step execution (one
	// contact passing through the step).
	CostCampaignAIStep = 1

	// CostInboxAgentThread is one inbound thread handled by the inbox agent.
	CostInboxAgentThread = 5

	// CostWebSearch is one web-search lookup made on behalf of an AI step
	// (charged only when the search returned results).
	CostWebSearch = 1
)

// Usage-based metering. The per-feature constants above are the up-front
// MINIMUM charged before the provider call (they double as the reservation
// that gates an empty balance); after the call, the real token usage is priced
// with MeteredCost and any overage beyond the minimum is settled from the
// ledger (draining to zero at worst — a delivered result never fails on the
// settle). So small calls cost exactly the flat minimum, and big calls pay for
// what they actually used.
const (
	// tokensPerCreditLight prices the light model tier (mini/haiku-class).
	tokensPerCreditLight = 1500
	// tokensPerCreditStandard prices the standard tier (4o/sonnet-class).
	tokensPerCreditStandard = 400
)

// lightModelMarkers identify light-tier models by name substring.
var lightModelMarkers = []string{"mini", "haiku", "nano", "flash", "lite"}

// TokensPerCredit returns how many tokens one credit buys on this model.
func TokensPerCredit(model string) int {
	m := strings.ToLower(model)
	for _, marker := range lightModelMarkers {
		if strings.Contains(m, marker) {
			return tokensPerCreditLight
		}
	}
	return tokensPerCreditStandard
}

// MeteredCost prices actual token usage on a model in credits (rounded up).
// Zero tokens price to zero: callers keep their flat minimum as the floor.
func MeteredCost(model string, tokens int) int {
	if tokens <= 0 {
		return 0
	}
	per := TokensPerCredit(model)
	return (tokens + per - 1) / per
}

// CreditPack is a fixed top-up pack purchasable through Stripe Checkout
// (mode=payment). Price IDs are configured per environment
// (STRIPE_CREDIT_PACK_<credits>_PRICE_ID); the pack sizes are product
// constants.
type CreditPack struct {
	Key     string `json:"key"`
	Credits int    `json:"credits"`
}

// CreditPacks are the three purchasable top-up packs.
var CreditPacks = []CreditPack{
	{Key: "pack_500", Credits: 500},
	{Key: "pack_2000", Credits: 2000},
	{Key: "pack_10000", Credits: 10000},
}

// PackByKey returns the pack for a key, or nil for an unknown key.
func PackByKey(key string) *CreditPack {
	for i := range CreditPacks {
		if CreditPacks[i].Key == key {
			return &CreditPacks[i]
		}
	}
	return nil
}
