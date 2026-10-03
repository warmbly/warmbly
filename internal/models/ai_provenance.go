package models

// AISurface names the AI surface an action originated from. It is recorded on
// audit metadata (tool-registry writes), credit-ledger attribution (in-mail AI
// steps), and automation run results, so "an AI did this" is queryable without
// guessing from the surrounding rows.
type AISurface string

const (
	AISurfaceAgent        AISurface = "agent"         // dashboard assistant run
	AISurfaceAdvisor      AISurface = "advisor"       // advisor apply / agent fix / autopilot
	AISurfaceMCP          AISurface = "mcp"           // POST /v1/mcp tools/call
	AISurfaceAPITools     AISurface = "api_tools"     // POST /ai/tools/:name/call
	AISurfaceSequenceAI   AISurface = "sequence_ai"   // in-campaign switch / agent steps
	AISurfaceAutomationAI AISurface = "automation_ai" // automation graph AI nodes
)

// AIDecision records how an AI-caused write was authorized.
type AIDecision string

const (
	AIDecisionHumanApproved AIDecision = "human_approved" // a person approved the paused tool call
	AIDecisionAlwaysAllow   AIDecision = "always_allow"   // org policy pre-approved the write tool
	AIDecisionAllowlisted   AIDecision = "allowlisted"    // user-authored allowlist inside a node config
	AIDecisionPermissionBit AIDecision = "permission_bit" // non-interactive surface; the caller's permission bits are the authority
)

// AI provenance metadata keys shared by audit metadata and structured records.
const (
	MetaKeyAISurface  = "ai_surface"
	MetaKeyAIDecision = "ai_decision"
	MetaKeyAISession  = "ai_session_id"
	MetaKeyAIMessage  = "ai_message_id"
)
