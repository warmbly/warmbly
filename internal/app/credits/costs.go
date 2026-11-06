package credits

import "github.com/warmbly/warmbly/internal/models"

// What each kind of work is expected to cost, checked against the balance
// before it starts. The charge afterwards is the actual cost (Price); these
// only decide whether there is enough to begin, so they sit at a typical
// call's price, not a worst case. The "Balance and usage" guide in docs/
// mirrors this table.
const (
	// EstimateWritingAssistant is one writing-assistant generation.
	EstimateWritingAssistant models.Micro = 10_000
	// EstimateAgentIteration is one iteration of an agent loop.
	EstimateAgentIteration models.Micro = 20_000
	// EstimateReplyDraft is one context-grounded reply draft.
	EstimateReplyDraft models.Micro = 20_000
	// EstimateResearchRun is one contact research run.
	EstimateResearchRun models.Micro = 50_000
	// EstimateAINode is one single-shot AI step in an automation or a campaign.
	EstimateAINode models.Micro = 10_000
	// EstimateInboxAgentThread is one inbound thread handled by the inbox agent.
	EstimateInboxAgentThread models.Micro = 50_000
	// EstimateSpamAnalysis is one AI spam analysis of a template.
	EstimateSpamAnalysis models.Micro = 20_000
)

// MinBalance is the least a workspace needs to start any metered work.
const MinBalance models.Micro = 10_000

// Top-up and automatic reload bounds, in micro-dollars.
const (
	TopUpMin models.Micro = 5 * models.MicroPerUSD
	TopUpMax models.Micro = 5_000 * models.MicroPerUSD

	ReloadThresholdMin    models.Micro = 10_000
	ReloadThresholdMax    models.Micro = 1_000 * models.MicroPerUSD
	ReloadMonthlyLimitMax models.Micro = 50_000 * models.MicroPerUSD

	DefaultReloadThreshold    models.Micro = 5 * models.MicroPerUSD
	DefaultReloadAmount       models.Micro = 25 * models.MicroPerUSD
	DefaultReloadMonthlyLimit models.Micro = 250 * models.MicroPerUSD
	DefaultLowBalance         models.Micro = 500_000
)
