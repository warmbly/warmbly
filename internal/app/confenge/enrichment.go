package confenge

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// Enrichment terminal after one automatic pass.
const (
	EnrichmentResolved    = "RESOLVED"
	EnrichmentUnavailable = "UNAVAILABLE"
	EnrichmentExhausted   = "EXHAUSTED"
)

// EnrichmentAttempt is one automatic recovery pass. It never invents evidence.
type EnrichmentAttempt struct {
	Attempted   bool     `json:"attempted"`
	Path        string   `json:"path,omitempty"`
	Status      string   `json:"status"`
	Resolved    bool     `json:"resolved"`
	Missing     []string `json:"missing,omitempty"`
	NextAction  string   `json:"next_action,omitempty"`
	ReasonCodes []string `json:"reason_codes,omitempty"`
}

// EnrichmentInput is the allowed local material for recovery.
type EnrichmentInput struct {
	Account     *models.OutreachAccount
	Candidates  []models.OutreachContactCandidate
	Evidence    []models.OutreachEvidence
	Now         time.Time
	Unavailable bool // fail-closed when the allowed pipeline is down
}

// AttemptEnrichment tries recoverable gaps only from already-published material.
// It does not call extra-cli, scrape, or invent identity.
func AttemptEnrichment(in EnrichmentInput) (RecipientResolution, OutboundMessagePlan, EnrichmentAttempt) {
	att := EnrichmentAttempt{Attempted: true, Path: "local_published_material"}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	if in.Unavailable {
		att.Status = EnrichmentUnavailable
		att.NextAction = "Reprocessar quando o pipeline de enriquecimento estiver disponível."
		att.ReasonCodes = []string{"enrichment_unavailable"}
		rec := ResolveRecipient(in.Account, in.Candidates, in.Now)
		_, plan := BuildOutboundPlan(MustPlaybook(), in.Account, pickFirstCandidate(in.Candidates), in.Evidence, 1)
		return rec, plan, att
	}

	// Recoverable: pick a stronger published candidate (named over generic).
	rewritten := rewriteCandidatesFromPublished(in.Candidates)
	rec := ResolveRecipient(in.Account, rewritten, in.Now)

	pb := MustPlaybook()
	var cand *models.OutreachContactCandidate
	if rec.State == RecipientValidated {
		cand = findCandidateByEmail(rewritten, rec.Email)
	} else {
		cand = pickFirstCandidate(rewritten)
	}
	_, plan := BuildOutboundPlan(pb, in.Account, cand, in.Evidence, 1)

	if rec.State == RecipientValidated && plan.Messageability == MessageabilityReady {
		att.Status = EnrichmentResolved
		att.Resolved = true
		return rec, plan, att
	}

	att.Status = EnrichmentExhausted
	if rec.State != RecipientValidated {
		att.Missing = append(att.Missing, "validated_recipient")
		att.ReasonCodes = appendUnique(att.ReasonCodes, rec.ReasonCodes...)
	}
	if plan.Messageability != MessageabilityReady {
		att.Missing = append(att.Missing, "mentionable_hook")
		att.ReasonCodes = appendUnique(att.ReasonCodes, plan.ReasonCodes...)
	}
	att.NextAction = firstNonEmpty(rec.NextAction, plan.Reason,
		"Não há enriquecimento automático possível sem nova evidência do extra-cli.")
	return rec, plan, att
}

func rewriteCandidatesFromPublished(cands []models.OutreachContactCandidate) []models.OutreachContactCandidate {
	out := make([]models.OutreachContactCandidate, len(cands))
	copy(out, cands)
	return out
}

func pickFirstCandidate(cands []models.OutreachContactCandidate) *models.OutreachContactCandidate {
	if len(cands) == 0 {
		return nil
	}
	return &cands[0]
}

func findCandidateByEmail(cands []models.OutreachContactCandidate, email string) *models.OutreachContactCandidate {
	want := canonicalPilotEmail(email)
	for i := range cands {
		if canonicalPilotEmail(cands[i].Email) == want {
			return &cands[i]
		}
	}
	return pickFirstCandidate(cands)
}

func mentionableEvidenceSummary(ev []models.OutreachEvidence, fact string) string {
	if s := strings.TrimSpace(fact); s != "" && !looksLikeMetadataDump(s) {
		return s
	}
	for _, e := range ev {
		s := firstNonEmpty(e.Synthesis, e.Excerpt, e.Title)
		if s != "" && !looksLikeMetadataDump(s) {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
