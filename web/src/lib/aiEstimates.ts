// Typical dollar cost of each AI action at the default prices, from the Estimate*
// values in internal/app/credits/costs.go and the balance guide's typical charges.
// The charge is the call's actual usage; these only feed "about $0.01" copy.
export const AI_ESTIMATE_USD = {
    writingAssistant: 0.01,
    agentIteration: 0.02,
    replyDraft: 0.02,
    researchRun: 0.09,
    aiNode: 0.01,
    inboxAgentThread: 0.05,
    spamAnalysis: 0.02,
    webSearch: 0.01,
} as const;
