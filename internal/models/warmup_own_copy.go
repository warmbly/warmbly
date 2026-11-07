package models

import "github.com/google/uuid"

// WarmupContentSourceCampaign marks a warmup thread opened with the copy of
// one of the sender's running campaigns, rendered as the campaign sends it.
const WarmupContentSourceCampaign = "campaign"

// Own copy is automatic: no workspace setting turns it on or sizes it.
const (
	// WarmupOwnCopyMinDays is how long a mailbox warms on ordinary
	// conversations before any of its own campaign copy goes through it.
	WarmupOwnCopyMinDays = 7
	// WarmupOwnCopySharePercent is the share of a mature sender's new warmup
	// threads that open with its campaign copy when some is eligible.
	WarmupOwnCopySharePercent = 25
	// WarmupOwnCopyTurns is an own-copy thread's length: the opening, the
	// recipient's reply and the sender's answer.
	WarmupOwnCopyTurns = 3
)

// WarmupCampaignCopyCandidate is one arm of an opening campaign step that a
// sender's warmup may carry: the step's own copy (VariantID nil) or one of
// its active A/B variants, as written, before rendering.
type WarmupCampaignCopyCandidate struct {
	CampaignID uuid.UUID
	SequenceID uuid.UUID
	VariantID  *uuid.UUID
	Subject    string
	BodyHTML   string
	BodyPlain  string
}

// WarmupCopyPlacement counts where warmup threads opened with a piece of copy
// landed, as the recipient mailboxes reported them. Sent counts every
// dispatched opening; the landing counters cover the receipts that came back.
type WarmupCopyPlacement struct {
	Sent int `json:"sent"`
	// Reported counts the sends whose recipient filed a receipt; landing rates are read over it.
	Reported int `json:"reported"`
	Inbox    int `json:"inbox"`
	Tabs     int `json:"tabs"`
	Spam     int `json:"spam"`
	Rescued  int `json:"rescued"`
}

// WarmupCopyGroupPlacement is WarmupCopyPlacement for one recipient provider
// group: google (Google Workspace) or microsoft (Microsoft 365).
type WarmupCopyGroupPlacement struct {
	Group string `json:"group"`
	WarmupCopyPlacement
}

// WarmupStepPlacement is where one arm of a campaign step landed in warmup.
type WarmupStepPlacement struct {
	SequenceID uuid.UUID  `json:"sequence_id"`
	VariantID  *uuid.UUID `json:"variant_id"`
	// Placement counts business inboxes on Google Workspace and Microsoft 365, the only ones campaign copy is sent to.
	Placement WarmupCopyPlacement        `json:"placement"`
	ByGroup   []WarmupCopyGroupPlacement `json:"by_group"`
}

// TotalCopyPlacement adds up a step arm's provider groups.
func TotalCopyPlacement(groups []WarmupCopyGroupPlacement) WarmupCopyPlacement {
	var t WarmupCopyPlacement
	for _, g := range groups {
		t.Sent += g.Sent
		t.Reported += g.Reported
		t.Inbox += g.Inbox
		t.Tabs += g.Tabs
		t.Spam += g.Spam
		t.Rescued += g.Rescued
	}
	return t
}
