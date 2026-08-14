package confenge

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	DecisionApprove         = "APPROVE"
	DecisionEdit            = "EDIT"
	DecisionReject          = "REJECT"
	DecisionRecipientChange = "RECIPIENT_CHANGE"
	DecisionSkip            = "SKIP"
)

// Known human-edit/reject reason codes. Never silent.
var HumanReasonCodes = []string{
	"generic_copy",
	"weak_hook",
	"unsupported_claim",
	"wrong_service",
	"wrong_recipient",
	"wrong_person",
	"wrong_hook",
	"channel_preference",
	"contact_correction",
	"gatekeeper_reached",
	"referral",
	"wrong_person",
	"invalid_route",
	"person_confirmed",
	"route_confirmed",
	"preferred_channel",
	"dnc",
	"awkward_tone",
	"excessive_metadata",
	"unclear_value",
	"bad_cta",
	"evidence_insufficient",
	"other",
}

// HumanCorrection is one operator decision on a sendable item.
type HumanCorrection struct {
	Decision        string    `json:"decision"`
	DraftID         string    `json:"draft_id,omitempty"`
	ActorID         string    `json:"actor_id,omitempty"`
	At              time.Time `json:"at"`
	ReasonCodes     []string  `json:"reason_codes,omitempty"`
	BeforeBody      string    `json:"before_body,omitempty"`
	AfterBody       string    `json:"after_body,omitempty"`
	BeforeSubject   string    `json:"before_subject,omitempty"`
	AfterSubject    string    `json:"after_subject,omitempty"`
	RecipientBefore string    `json:"recipient_before,omitempty"`
	RecipientAfter  string    `json:"recipient_after,omitempty"`
	ContentVersion  string    `json:"content_version,omitempty"`
	ContextVersion  string    `json:"context_version,omitempty"`
	RecipientVer    string    `json:"recipient_version,omitempty"`
	PolicyVersion   string    `json:"policy_version,omitempty"`
	Silent          bool      `json:"silent"`
	// Additive interaction corrections (person/role/route/DNC). Never
	// overwrite upstream evidence; source is always HUMAN_INTERACTION.
	Kind   string `json:"kind,omitempty"`
	Source string `json:"source,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// EvaluationRecord binds a human decision to the ledger chain.
type EvaluationRecord struct {
	EvaluationID  string          `json:"evaluation_id"`
	ActionID      string          `json:"action_id,omitempty"`
	MessageID     string          `json:"message_id,omitempty"`
	TargetID      string          `json:"target_id,omitempty"`
	Decision      string          `json:"decision"`
	Correction    HumanCorrection `json:"correction"`
	AISubstituted bool            `json:"ai_substituted"`
}

// RecordHumanDecision builds a persistent correction. Silent approval is rejected.
func RecordHumanDecision(decision, draftID, actorID, beforeBody, afterBody, beforeSubj, afterSubj string, reasons []string) (HumanCorrection, error) {
	d := strings.ToUpper(strings.TrimSpace(decision))
	switch d {
	case DecisionApprove, DecisionEdit, DecisionReject, DecisionRecipientChange, DecisionSkip:
	default:
		return HumanCorrection{}, errHuman("unknown_decision")
	}
	if strings.TrimSpace(draftID) == "" || strings.TrimSpace(actorID) == "" {
		return HumanCorrection{}, errHuman("missing_actor_or_draft")
	}
	hc := HumanCorrection{
		Decision:      d,
		DraftID:       draftID,
		ActorID:       actorID,
		At:            time.Now().UTC(),
		ReasonCodes:   normalizeHumanReasons(reasons),
		BeforeBody:    beforeBody,
		AfterBody:     afterBody,
		BeforeSubject: beforeSubj,
		AfterSubject:  afterSubj,
		PolicyVersion: OutreachDoctrineVersion + "/" + RecipientPolicyVersion,
		Silent:        false,
	}
	if d == DecisionEdit && beforeBody == afterBody && beforeSubj == afterSubj {
		return HumanCorrection{}, errHuman("edit_without_diff")
	}
	return hc, nil
}

func attachHumanCorrection(d *models.OutreachDraft, hc HumanCorrection) {
	if d == nil {
		return
	}
	var val ValidationResult
	if len(d.ValidationJSON) > 0 {
		_ = json.Unmarshal(d.ValidationJSON, &val)
	}
	hcCopy := hc
	val.HumanCorrection = &hcCopy
	if packed, err := json.Marshal(val); err == nil {
		d.ValidationJSON = packed
	}
}

func normalizeHumanReasons(in []string) []string {
	var out []string
	for _, r := range in {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" {
			continue
		}
		ok := false
		for _, known := range HumanReasonCodes {
			if r == known {
				ok = true
				break
			}
		}
		if !ok {
			r = "other"
		}
		out = appendUnique(out, r)
	}
	return out
}

type humanError string

func (e humanError) Error() string { return string(e) }

func errHuman(code string) error { return humanError(code) }
