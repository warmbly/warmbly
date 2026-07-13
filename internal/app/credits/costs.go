package credits

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

	// CostAutomationAINode is one ai_classify/ai_extract/ai_generate node execution.
	CostAutomationAINode = 1

	// CostInboxAgentThread is one inbound thread handled by the inbox agent.
	CostInboxAgentThread = 5
)

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
