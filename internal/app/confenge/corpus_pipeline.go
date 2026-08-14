package confenge

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// CorpusAuditRow is one honest line for a pilot account.
type CorpusAuditRow struct {
	Company             string `json:"company"`
	CanonicalTargetID   string `json:"canonical_target_id,omitempty"`
	CanonicalContactID  string `json:"canonical_contact_id,omitempty"`
	Service             string `json:"service"`
	WhyThisAccount      string `json:"why_this_account,omitempty"`
	WhyNow              string `json:"why_now,omitempty"`
	MentionableEvidence string `json:"mentionable_evidence,omitempty"`
	RecipientState      string `json:"recipient_state"`
	MessageabilityState string `json:"messageability_state"`
	FinalState          string `json:"final_state"`
	Blocker             string `json:"blocker,omitempty"`
	NextAction          string `json:"next_action,omitempty"`
	SendableBody        string `json:"sendable_body,omitempty"`
	Queue               string `json:"queue"`
}

// CorpusAuditReport is the campaign terminal snapshot.
type CorpusAuditReport struct {
	Selected               int              `json:"selected"`
	RecipientValidated     int              `json:"recipient_validated"`
	RecipientException     int              `json:"recipient_exception"`
	RecipientBlocked       int              `json:"recipient_blocked"`
	MessageReady           int              `json:"message_ready"`
	NeedsEnrichment        int              `json:"needs_enrichment"`
	MessageBlocked         int              `json:"message_blocked"`
	HumanDecisionsRequired int              `json:"human_decisions_required"`
	LegacyDraftsRemaining  int              `json:"legacy_drafts_remaining"`
	UnexplainedState       int              `json:"unexplained_state"`
	Rows                   []CorpusAuditRow `json:"rows"`
}

const (
	ReviewQueueReady      = "ready_review"
	ReviewQueueException  = "exception"
	ReviewQueueEnrichment = "enrichment"
	ReviewQueueBlocked    = "blocked"
)

// ProcessPilotAccount evaluates recipient + messageability + enrichment.
// READY requires a validated human recipient AND sendable copy.
func ProcessPilotAccount(
	acc *models.OutreachAccount,
	cands []models.OutreachContactCandidate,
	ev []models.OutreachEvidence,
	now time.Time,
) CorpusAuditRow {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	pb := MustPlaybook()
	rec := ResolveRecipient(acc, cands, now)
	cand := pickResolvedCandidate(cands, rec)
	st, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)

	if rec.State != RecipientValidated || plan.Messageability != MessageabilityReady {
		_, plan2, att := AttemptEnrichment(EnrichmentInput{
			Account: acc, Candidates: cands, Evidence: ev, Now: now,
		})
		if att.Resolved {
			rec = ResolveRecipient(acc, cands, now)
			plan = plan2
		}
	}

	out := ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
	row := CorpusAuditRow{
		Company:             firstNonEmpty(accName(acc), rec.Company),
		CanonicalTargetID:   rec.CanonicalTargetID,
		CanonicalContactID:  rec.CanonicalContactID,
		Service:             firstNonEmpty(plan.ServiceCode, accService(acc)),
		WhyThisAccount:      st.WhyThisAccount,
		WhyNow:              st.WhyNow,
		MentionableEvidence: mentionableEvidenceSummary(ev, firstNonEmpty(plan.Hook, st.ObservedFact)),
		RecipientState:      rec.State,
		MessageabilityState: plan.Messageability,
	}

	switch {
	case rec.State == RecipientValidated && plan.Messageability == MessageabilityReady && strings.TrimSpace(out.BodyText) != "":
		row.FinalState = MessageabilityReady
		row.SendableBody = out.BodyText
		row.Queue = ReviewQueueReady
	case rec.State == RecipientException:
		row.FinalState = MessageabilityNeedsEnrichment
		row.Blocker = firstNonEmpty(rec.Reason, strings.Join(rec.ReasonCodes, ","))
		row.NextAction = rec.NextAction
		row.Queue = ReviewQueueException
	case rec.State == RecipientBlocked || plan.Messageability == MessageabilityBlocked:
		row.FinalState = MessageabilityBlocked
		row.Blocker = firstNonEmpty(rec.Reason, plan.Reason, strings.Join(append(rec.ReasonCodes, plan.ReasonCodes...), ","))
		row.NextAction = firstNonEmpty(rec.NextAction, "Não abordar.")
		row.Queue = ReviewQueueBlocked
	default:
		row.FinalState = MessageabilityNeedsEnrichment
		row.Blocker = firstNonEmpty(plan.Reason, rec.Reason)
		row.NextAction = firstNonEmpty(plan.Reason, rec.NextAction)
		row.Queue = ReviewQueueEnrichment
	}

	if row.RecipientState == "" || row.MessageabilityState == "" || row.FinalState == "" {
		row.FinalState = MessageabilityBlocked
		row.Blocker = "unexplained_state"
		row.Queue = ReviewQueueBlocked
	}
	if row.FinalState != MessageabilityReady {
		row.SendableBody = ""
	}
	return row
}

// SummarizeCorpus folds rows into campaign counts. READY never includes generic.
func SummarizeCorpus(rows []CorpusAuditRow, legacyDrafts int) CorpusAuditReport {
	rep := CorpusAuditReport{Rows: rows, Selected: len(rows), LegacyDraftsRemaining: legacyDrafts}
	for _, r := range rows {
		switch r.RecipientState {
		case RecipientValidated:
			rep.RecipientValidated++
		case RecipientException:
			rep.RecipientException++
		case RecipientBlocked:
			rep.RecipientBlocked++
		default:
			rep.UnexplainedState++
		}
		switch r.FinalState {
		case MessageabilityReady:
			if r.SendableBody == "" {
				rep.UnexplainedState++
				rep.NeedsEnrichment++
				continue
			}
			rep.MessageReady++
			rep.HumanDecisionsRequired++
		case MessageabilityNeedsEnrichment:
			rep.NeedsEnrichment++
		case MessageabilityBlocked:
			rep.MessageBlocked++
		default:
			rep.UnexplainedState++
		}
	}
	return rep
}

// ProcessFeedCorpus evaluates the first n leads of a native feed (real snapshot).
func ProcessFeedCorpus(feed *Feed, n int, now time.Time) CorpusAuditReport {
	if feed == nil {
		return CorpusAuditReport{}
	}
	if n <= 0 || n > len(feed.Leads) {
		n = len(feed.Leads)
	}
	rows := make([]CorpusAuditRow, 0, n)
	for i := 0; i < n; i++ {
		acc, cands, ev := feedLeadToModels(feed.Leads[i])
		rows = append(rows, ProcessPilotAccount(acc, cands, ev, now))
	}
	return SummarizeCorpus(rows, 0)
}

func feedLeadToModels(lead FeedLead) (*models.OutreachAccount, []models.OutreachContactCandidate, []models.OutreachEvidence) {
	acc := &models.OutreachAccount{
		SourceLeadID:      lead.SourceLeadID,
		CNPJ14:            lead.Company.CNPJ14,
		RazaoSocial:       lead.Company.RazaoSocial,
		NomeFantasia:      lead.Company.NomeFantasia,
		UF:                lead.Company.UF,
		Municipio:         lead.Company.Municipio,
		ServiceCode:       lead.Offer.ServiceCode,
		ServiceName:       lead.Offer.ServiceName,
		MomentCode:        lead.Moment.Code,
		MomentSummary:     lead.Moment.Summary,
		FactToMention:     lead.MessagingContext.FactToMention,
		QuestionToAsk:     lead.MessagingContext.QuestionToAsk,
		CTA:               lead.MessagingContext.CTA,
		ClaimsToAvoid:     lead.MessagingContext.ClaimsToAvoid,
		MomentEvidenceIDs: lead.Moment.EvidenceIDs,
		DoNotContact:      strings.EqualFold(lead.CommercialState, "DO_NOT_CONTACT"),
		Blocked:           strings.EqualFold(lead.CommercialState, "BLOCKED"),
	}
	cands := make([]models.OutreachContactCandidate, 0, len(lead.Contacts))
	for _, fc := range lead.Contacts {
		c := leadToCandidate(uuid.Nil, uuid.Nil, uuid.Nil, fc)
		cands = append(cands, *c)
	}
	ev := make([]models.OutreachEvidence, 0, len(lead.Evidence))
	for _, fe := range lead.Evidence {
		e := leadToEvidence(uuid.Nil, uuid.Nil, uuid.Nil, fe)
		ev = append(ev, *e)
	}
	return acc, cands, ev
}

func pickResolvedCandidate(cands []models.OutreachContactCandidate, rec RecipientResolution) *models.OutreachContactCandidate {
	if rec.Email != "" {
		if c := findCandidateByEmail(cands, rec.Email); c != nil {
			return c
		}
	}
	return pickFirstCandidate(cands)
}

func accName(acc *models.OutreachAccount) string {
	if acc == nil {
		return ""
	}
	return firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
}

func accService(acc *models.OutreachAccount) string {
	if acc == nil {
		return ""
	}
	return acc.ServiceCode
}
