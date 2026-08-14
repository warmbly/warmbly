package confenge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// WorkingQueueLane labels operational readiness (not commercial intelligence).
const (
	LaneNeedsAttention = "needs_attention"
	LaneNow            = "agora"
	LaneNeedsContact   = "needs_contact"
	LaneNeedsReview    = "needs_review"
	LaneApproved       = "approved_scheduled"
	LaneWatch          = "aguardar"
)

// WorkingQueueItem is one row in the dynamic operational queue.
type WorkingQueueItem struct {
	Account          models.OutreachAccount `json:"account"`
	Lane             string                 `json:"lane"`
	WhyNow           string                 `json:"why_now,omitempty"`
	ReasonCodes      []string               `json:"reason_codes,omitempty"`
	ActivationScore  float64                `json:"activation_score,omitempty"`
	NextBestActionAt *time.Time             `json:"next_best_action_at,omitempty"`
	ExpiresAt        *time.Time             `json:"activation_expires_at,omitempty"`
	ContactReady     bool                   `json:"contact_ready"`
	ContextStale     bool                   `json:"context_stale"`
	ChannelReadiness string                 `json:"channel_readiness,omitempty"`
}

// WorkingQueueSummary is the operator cockpit counters (real numbers).
type WorkingQueueSummary struct {
	ReservoirMonitored int `json:"reservoir_monitored"`
	ActionableNow      int `json:"actionable_now"`
	NeedsContact       int `json:"needs_contact"`
	NeedsReview        int `json:"needs_review"`
	ApprovedScheduled  int `json:"approved_scheduled"`
	WatchAwaiting      int `json:"watch_awaiting"`
	Suppressed         int `json:"suppressed"`
	StaleContext       int `json:"stale_context"`
	DueNext24h         int `json:"due_next_24h"`
	// Capacity planning (not governor enforcement)
	TheoreticalSlots24h int        `json:"theoretical_slots_24h"`
	CapacityLoad        float64    `json:"capacity_load"`
	DynamicPriority     bool       `json:"dynamic_priority_enabled"`
	LastFeedSyncAt      *time.Time `json:"last_feed_sync_at,omitempty"`
	FeedAgeSeconds      *int64     `json:"feed_age_seconds,omitempty"`
}

// applyActivationToAccount copies optional feed activation onto the account model.
// Does not recompute commercial intelligence.
func applyActivationToAccount(acc *models.OutreachAccount, lead FeedLead) {
	if acc == nil {
		return
	}
	acc.MessageContextHash = MessageContextHash(lead)
	if lead.Activation == nil {
		return
	}
	a := lead.Activation
	acc.ActivationState = strings.ToUpper(strings.TrimSpace(a.State))
	acc.ActivationScore = a.Score
	if acc.ActivationScore < 0 {
		acc.ActivationScore = 0
	}
	if acc.ActivationScore > 100 {
		acc.ActivationScore = 100
	}
	acc.ActivationReasonCodes = append([]string{}, a.ReasonCodes...)
	acc.ActivationPolicyVersion = SanitizeText(a.PolicyVersion, 100)
	acc.ActivationEvaluatedAt = parseTimePtr(a.EvaluatedAt)
	acc.NextBestActionAt = parseTimePtr(a.NextBestActionAt)
	acc.ActivationExpiresAt = parseTimePtr(a.ExpiresAt)
	acc.ActivationSourceHash = SanitizeText(a.SourceHash, 128)
	if a.ScoreComponents != nil {
		b, _ := json.Marshal(a.ScoreComponents)
		acc.ScoreComponentsJSON = b
	}
}

func parseTimePtr(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		u := t.UTC()
		return &u
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		u := t.UTC()
		return &u
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

// IsOutboundDue reports whether commercial activation says the account is due now.
// Warmbly still applies local suppression (DNC, bounce, active cadence, etc.).
func IsOutboundDue(acc *models.OutreachAccount, now time.Time) bool {
	if acc == nil {
		return false
	}
	if acc.DoNotContact || acc.Blocked {
		return false
	}
	if RequireTargetFit(acc) != nil {
		return false
	}
	switch acc.QueueState {
	case models.OutreachQueueDoNotContact, models.OutreachQueueBlocked, models.OutreachQueueBounced,
		models.OutreachQueueReplied, models.OutreachQueueMeeting, models.OutreachQueueProposal,
		models.OutreachQueueWon, models.OutreachQueueLost, models.OutreachQueueEnrolled,
		models.OutreachQueueSent:
		return false
	}
	if acc.ActivationState != ActivationActionableNow {
		return false
	}
	if acc.NextBestActionAt != nil && acc.NextBestActionAt.After(now) {
		return false
	}
	if acc.ActivationExpiresAt != nil && !acc.ActivationExpiresAt.After(now) {
		return false
	}
	return true
}

// ListWorkingQueue returns the operational queue. When dynamic priority is off,
// falls back to legacy priority_rank ordering for READY/NEEDS_CONTACT.
func (s *service) ListWorkingQueue(ctx context.Context, orgID uuid.UUID, lane string, limit int) ([]WorkingQueueItem, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	lane = normalizeLane(lane)
	now := time.Now().UTC()
	// SQL filters push due/activation predicates into the DB (not a 200-row sample).
	filter := repository.OutreachAccountFilter{
		Limit:           limit,
		DynamicPriority: s.cfg.DynamicPriorityEnabled,
		ExcludeTerminal: true,
	}
	switch lane {
	case LaneNow:
		filter.ActivationState = ActivationActionableNow
		filter.ActivationDueNow = true
		filter.ActivationNotExpired = true
		filter.RequireOperational = true
		// Prefer contact-ready operational states; needs_contact is its own lane.
		// Still list READY/REVIEW/APPROVED first via dynamic order.
	case LaneNeedsContact:
		filter.RequireTargetFitEligible = true
		filter.ActivationState = ActivationActionableNow
		filter.ActivationDueNow = true
		filter.ActivationNotExpired = true
		filter.QueueState = models.OutreachQueueNeedsContact
	case LaneNeedsReview:
		filter.RequireTargetFitEligible = true
		filter.QueueState = models.OutreachQueueNeedsReview
		filter.ExcludeTerminal = false
	case LaneApproved:
		filter.RequireTargetFitEligible = true
		filter.QueueState = models.OutreachQueueApproved
		filter.ExcludeTerminal = false
	case LaneWatch:
		filter.ActivationState = ActivationWatch
	case LaneNeedsAttention:
		// Human-dominant: replied/meeting/proposal — not terminal-excluded
		filter.ExcludeTerminal = false
	default:
		if !s.cfg.DynamicPriorityEnabled {
			// Legacy surface: classic ready / needs contact only.
			filter.ExcludeTerminal = false
		} else {
			filter.ActivationDueNow = true
			filter.ActivationNotExpired = true
		}
	}
	accounts, err := s.repo.ListAccounts(ctx, orgID, filter)
	if err != nil {
		return nil, errx.New(errx.Internal, "list accounts: "+err.Error())
	}
	// Needs attention: filter client-side only for multi-state human lanes.
	items := make([]WorkingQueueItem, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		if cands, err := s.repo.ListCandidates(ctx, orgID, acc.ID); err == nil {
			acc.Contacts = cands
		}
		item := workingItemFromAccount(&acc, now)
		if lane == LaneNeedsAttention && item.Lane != LaneNeedsAttention {
			continue
		}
		if lane == LaneNow && item.Lane != LaneNow {
			// SQL already filtered ACTIONABLE due; still drop pure needs-contact into its lane.
			if item.Lane == LaneNeedsContact {
				continue
			}
			// Allow ready/review that is also due actionable.
			if item.Lane != LaneNow && item.Lane != LaneNeedsReview && item.Lane != LaneApproved {
				if !IsOutboundDue(&acc, now) {
					continue
				}
				item.Lane = LaneNow
			}
		}
		if !s.cfg.DynamicPriorityEnabled && lane == "" {
			if acc.QueueState != models.OutreachQueueReadyToGenerate &&
				acc.QueueState != models.OutreachQueueNeedsContact &&
				acc.QueueState != models.OutreachQueueNeedsReview &&
				acc.QueueState != models.OutreachQueueApproved {
				continue
			}
		}
		items = append(items, item)
	}
	if s.cfg.DynamicPriorityEnabled || lane == LaneNow {
		sortWorkingQueue(items)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func normalizeLane(lane string) string {
	switch strings.ToLower(strings.TrimSpace(lane)) {
	case "", "all":
		return ""
	case "agora", "now", "actionable":
		return LaneNow
	case "needs_contact", "precisa_de_contato", "contact":
		return LaneNeedsContact
	case "needs_attention", "attention":
		return LaneNeedsAttention
	case "needs_review", "review":
		return LaneNeedsReview
	case "approved", "scheduled", "approved_scheduled":
		return LaneApproved
	case "aguardar", "watch":
		return LaneWatch
	default:
		return lane
	}
}

func workingItemFromAccount(acc *models.OutreachAccount, now time.Time) WorkingQueueItem {
	item := WorkingQueueItem{
		Account:          *acc,
		ReasonCodes:      append([]string{}, acc.ActivationReasonCodes...),
		ActivationScore:  acc.ActivationScore,
		NextBestActionAt: acc.NextBestActionAt,
		ExpiresAt:        acc.ActivationExpiresAt,
		WhyNow:           firstNonEmpty(acc.MomentSummary, strings.Join(acc.ActivationReasonCodes, ", ")),
	}
	// Dominant human states
	switch {
	case acc.DoNotContact || acc.QueueState == models.OutreachQueueDoNotContact:
		item.Lane = LaneWatch
	case acc.QueueState == models.OutreachQueueReplied || acc.QueueState == models.OutreachQueueMeeting ||
		acc.QueueState == models.OutreachQueueProposal:
		item.Lane = LaneNeedsAttention
	case acc.QueueState == models.OutreachQueueNeedsReview || acc.QueueState == models.OutreachQueueApproved:
		if acc.QueueState == models.OutreachQueueApproved {
			item.Lane = LaneApproved
		} else {
			item.Lane = LaneNeedsReview
		}
	case acc.QueueState == models.OutreachQueueNeedsContact || !hasAccountContactReady(acc):
		if IsOutboundDue(acc, now) || acc.ActivationState == ActivationActionableNow {
			item.Lane = LaneNeedsContact
		} else {
			item.Lane = LaneWatch
		}
		item.ContactReady = false
	case IsOutboundDue(acc, now):
		item.Lane = LaneNow
		item.ContactReady = true
	default:
		item.Lane = LaneWatch
		item.ContactReady = acc.QueueState == models.OutreachQueueReadyToGenerate
	}
	return item
}

func hasAccountContactReady(acc *models.OutreachAccount) bool {
	if acc == nil {
		return false
	}
	if acc.QueueState == models.OutreachQueueNeedsContact {
		return false
	}
	for _, c := range acc.Contacts {
		if RequireEmailOutbound(acc, &c) == nil {
			return true
		}
	}
	// Contacts may not be joined; treat READY_TO_GENERATE as ready.
	return false
}

func sortWorkingQueue(items []WorkingQueueItem) {
	// next_best_action_at ASC, activation_score DESC, priority_rank ASC, cnpj ASC
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if workingLess(items[j], items[i]) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func workingLess(a, b WorkingQueueItem) bool {
	// Lane priority: needs_attention > agora > needs_contact > needs_review > approved > watch
	lp := map[string]int{
		LaneNeedsAttention: 0, LaneNow: 1, LaneNeedsContact: 2,
		LaneNeedsReview: 3, LaneApproved: 4, LaneWatch: 5,
	}
	if lp[a.Lane] != lp[b.Lane] {
		return lp[a.Lane] < lp[b.Lane]
	}
	an, bn := a.NextBestActionAt, b.NextBestActionAt
	if an != nil && bn != nil && !an.Equal(*bn) {
		return an.Before(*bn)
	}
	if an != nil && bn == nil {
		return true
	}
	if an == nil && bn != nil {
		return false
	}
	if a.ActivationScore != b.ActivationScore {
		return a.ActivationScore > b.ActivationScore
	}
	if a.Account.PriorityRank != b.Account.PriorityRank {
		return a.Account.PriorityRank < b.Account.PriorityRank
	}
	return a.Account.CNPJ14 < b.Account.CNPJ14
}

// WorkingQueueOverview aggregates cockpit metrics without materializing 48k rows.
func (s *service) WorkingQueueOverview(ctx context.Context, orgID uuid.UUID) (*WorkingQueueSummary, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	sum, err := s.repo.CountByQueueState(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "summary: "+err.Error())
	}
	out := &WorkingQueueSummary{
		NeedsContact:       sum.NeedsContact,
		NeedsReview:        sum.NeedsReview,
		ApprovedScheduled:  sum.Approved + sum.Enrolled,
		DynamicPriority:    s.cfg.DynamicPriorityEnabled,
		ReservoirMonitored: sum.Total,
	}
	now := time.Now().UTC()
	if act, err := s.repo.CountByActivationState(ctx, orgID, now); err == nil && act != nil {
		out.ActionableNow = act.ActionableDueNow
		if act.NeedsContactDue > 0 {
			out.NeedsContact = act.NeedsContactDue
		}
		out.WatchAwaiting = act.Watch + act.ResearchRequired
		out.Suppressed = act.Suppressed
		out.DueNext24h = act.ActionableDueNow
		out.DueNext24h += sum.Approved
		if act.Total > out.ReservoirMonitored {
			out.ReservoirMonitored = act.Total
		}
	} else if out.WatchAwaiting == 0 && sum.Total > 0 {
		out.WatchAwaiting = sum.Total - sum.NeedsContact - sum.ReadyToGenerate - sum.NeedsReview -
			sum.Approved - sum.Enrolled - sum.Sent - sum.Replied - sum.Meeting - sum.Proposal -
			sum.Won - sum.Blocked - sum.Bounced - sum.DoNotContact
		if out.WatchAwaiting < 0 {
			out.WatchAwaiting = 0
		}
	}
	// Capacity theory from governor + window (does not change governor)
	slots := 10 * 9 // default ~10/h * 9h window
	if s.governor != nil {
		cap := s.governor.Config().SendsPerHour
		if cap > 0 {
			slots = cap * 9
		}
	}
	out.TheoreticalSlots24h = slots
	if out.DueNext24h == 0 {
		out.DueNext24h = sum.ReadyToGenerate + sum.Approved
	}
	if slots > 0 {
		out.CapacityLoad = float64(out.DueNext24h) / float64(slots)
	}
	if st, err := s.repo.GetFeedSyncState(ctx, orgID); err == nil && st != nil && st.LastStatus == "completed" {
		out.LastFeedSyncAt = st.SourceGeneratedAt
		if st.SourceGeneratedAt != nil {
			age := int64(time.Since(*st.SourceGeneratedAt).Seconds())
			out.FeedAgeSeconds = &age
		}
	}
	return out, nil
}

// AssertMessageContextFresh fails closed when approved content is based on stale context.
func AssertMessageContextFresh(acc *models.OutreachAccount, generatedHash string) error {
	if acc == nil {
		return fmt.Errorf("account required")
	}
	if strings.TrimSpace(acc.MessageContextHash) == "" {
		// Legacy accounts without context hash: allow (backward compatible).
		return nil
	}
	if strings.TrimSpace(generatedHash) == "" {
		// Drafts generated before this feature: treat as stale when account has hash.
		return fmt.Errorf("stale message context: generated_context_hash missing while account has message_context_hash")
	}
	if generatedHash != acc.MessageContextHash {
		return fmt.Errorf("stale message context: account data changed since generation/approval — regenerate and re-approve")
	}
	return nil
}

// ApplyDeactivations revokes unsent work for accounts removed from the current actionable set.
func (s *service) ApplyDeactivations(ctx context.Context, orgID uuid.UUID, deacts []map[string]any) (int, error) {
	n := 0
	for _, d := range deacts {
		cnpj, _ := d["cnpj14"].(string)
		cnpj = NormalizeCNPJ14(cnpj)
		if cnpj == "" {
			return n, fmt.Errorf("deactivation has invalid cnpj14")
		}
		acc, err := s.repo.GetAccountByCNPJ(ctx, orgID, cnpj)
		if err != nil {
			return n, fmt.Errorf("load deactivation account %s: %w", cnpj, err)
		}
		if acc == nil {
			continue
		}
		toState, _ := d["to_state"].(string)
		if toState == "" {
			toState = ActivationWatch
		}
		acc.ActivationState = strings.ToUpper(toState)
		if acc.ActivationState == ActivationActionableNow {
			return n, fmt.Errorf("deactivation %s cannot target ACTIONABLE_NOW", cnpj)
		}
		// Do not alter queue_state for terminal/human states
		switch acc.QueueState {
		case models.OutreachQueueDoNotContact, models.OutreachQueueBlocked, models.OutreachQueueBounced,
			models.OutreachQueueReplied, models.OutreachQueueMeeting, models.OutreachQueueProposal,
			models.OutreachQueueWon, models.OutreachQueueLost, models.OutreachQueueSent, models.OutreachQueueEnrolled:
			// still update activation projection only via upsert fields if we had a partial update API
		}
		// Soft-update: re-upsert with existing queue_state
		acc.NextBestActionAt = nil
		acc.TargetFitFresh = false
		acc.TargetFitEligible = false
		acc.TargetFitFreshnessReason = TargetFitReasonDeactivated
		acc.TargetFitSuppressionReason = TargetFitReasonDeactivated
		if !isHistoricalTerminalQueue(acc.QueueState) {
			acc.QueueState = models.OutreachQueueTargetFitSuppressed
		}
		if _, err := s.repo.UpsertAccount(ctx, acc); err != nil {
			return n, fmt.Errorf("persist deactivation %s: %w", cnpj, err)
		}
		if _, err := s.repo.InvalidateAccountOutboundForTargetFit(ctx, orgID, acc.ID, TargetFitReasonDeactivated); err != nil {
			return n, fmt.Errorf("revoke deactivated outbound %s: %w", cnpj, err)
		}
		n++
	}
	return n, nil
}

// Ensure service interface still compiles if methods are used from handlers.
var _ = uuid.Nil
