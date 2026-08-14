package confenge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrCampaignTouchpointNotFound distinguishes ordinary campaign EMAIL_SENT
// events from an invalid CONFENGE enrollment projection.
var ErrCampaignTouchpointNotFound = errors.New("confenge touchpoint enrollment not found")

// GateKind is a closed set of campaign-email gate outcomes.
// Permanent suppress/bounce is only valid for GateHardBlock.
type GateKind int

const (
	// GateProceed: reservation granted; caller may send then Commit/Release.
	GateProceed GateKind = iota
	// GateDeferred: no slot (cap/gap/pause/window); reschedule at NextSlot; no success count.
	GateDeferred
	// GateAlready: successful outbound already recorded for this message_key (idempotent).
	GateAlready
	// GateHardBlock: DNC/bounce/account block only — permanent progress skip + suppress.
	GateHardBlock
	// GateCommercialBlock: reversible target-fit/readiness block; skip without global suppression.
	GateCommercialBlock
	// GateTransient: governor/store/infra failure — retry/backoff; never suppress/bounce.
	GateTransient
	// GateBypass: not a CONFENGE campaign or governor not wired; send without global cap.
	GateBypass
)

// Machine-readable reasons for GateHardBlock / GateDeferred / GateTransient.
const (
	ReasonDNCOrBounce = "dnc_or_bounce"
	ReasonAccountDNC  = "account_dnc"
	ReasonAlreadySent = "already_sent"
	ReasonNotConfenge = "not_confenge"
	ReasonNoGovernor  = "no_governor"
	ReasonGovernor    = "governor_error"
	ReasonTargetFit   = "target_fit_not_operational"
	ReasonSendingOff  = "sending_paused"
)

// CampaignGateResult is the discriminant result of GateCampaignEmail.
// Policy hard-blocks never use Err; infrastructure failures use Kind=GateTransient + Err.
type CampaignGateResult struct {
	Kind          GateKind
	ReservationID uuid.UUID
	NextSlot      time.Time
	Reason        string
	Err           error // only meaningful for GateTransient
}

// PermanentSuppress reports whether the campaign task may org-wide suppress / bounce-mark.
// Only GateHardBlock is true; Transient/Deferred/Already/Proceed never permanent-suppress.
func (r CampaignGateResult) PermanentSuppress() bool {
	return r.Kind == GateHardBlock
}

// IsConfengeCampaign reports whether a campaign is CONFENGE-attributed.
func IsConfengeCampaign(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), "CONFENGE")
}

// MessageKeyCampaignEmail builds a stable idempotency key for a campaign step send.
func MessageKeyCampaignEmail(campaignID, contactID, sequenceID uuid.UUID) string {
	return fmt.Sprintf("email:campaign:%s:contact:%s:seq:%s", campaignID, contactID, sequenceID)
}

// GateCampaignEmail is the final CONFENGE email outbound gate (pre worker/SMTP).
// Outcomes are a closed GateKind set — callers must switch on Kind, not raw error.
// CONFENGE campaigns never fail-open: missing/unhealthy governor is GateTransient (zero send).
// Non-CONFENGE campaigns retain legacy GateBypass.
//
// CAMPAIGN_POLICY revalidation always runs for open transportable touchpoints on
// the resolved account, independent of MessageContextHash presence.
func (s *service) GateCampaignEmail(ctx context.Context, orgID uuid.UUID, campaignName, recipientEmail string, campaignID, contactID, sequenceID uuid.UUID) CampaignGateResult {
	isConfenge := IsConfengeCampaign(campaignName)
	var enrolledTouchpoint *models.OutreachTouchpoint
	if s != nil && s.repo != nil && campaignID != uuid.Nil {
		settings, err := s.repo.GetOrgSettings(ctx, orgID)
		if err != nil {
			return CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: fmt.Errorf("CONFENGE campaign attribution lookup: %w", err)}
		}
		if settings != nil && settings.CampaignID != nil && *settings.CampaignID == campaignID {
			isConfenge = true
		}
	}
	if s != nil && s.repo != nil && campaignID != uuid.Nil && contactID != uuid.Nil {
		var err error
		enrolledTouchpoint, err = s.repo.GetTouchpointByEnrollment(ctx, orgID, campaignID, contactID)
		if err != nil {
			return CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: fmt.Errorf("touchpoint enrollment lookup: %w", err)}
		}
		isConfenge = isConfenge || enrolledTouchpoint != nil
	}
	if !isConfenge {
		return CampaignGateResult{Kind: GateBypass, Reason: ReasonNotConfenge}
	}
	// Re-check the process/file kill switch for every campaign task. This blocks
	// stale campaign leads that were enrolled before an operator paused sending.
	if s == nil || !s.cfg.SendingAllowed() {
		return CampaignGateResult{Kind: GateDeferred, Reason: ReasonSendingOff, NextSlot: time.Now().UTC().Add(time.Minute)}
	}
	// CONFENGE path: fail-closed without a healthy governor. Never GateBypass.
	if s == nil || !s.cfg.Enabled || s.governor == nil {
		return CampaignGateResult{
			Kind:   GateTransient,
			Reason: ReasonNoGovernor,
			Err:    fmt.Errorf("confenge governor not wired or disabled; refusing send"),
		}
	}

	// Dominant blocks: DNC/opt-out/bounce — hard block without consuming a slot.
	var acc *models.OutreachAccount
	var cand *models.OutreachContactCandidate
	if campaignID != uuid.Nil && contactID != uuid.Nil {
		c, found, err := s.repo.FindCandidateByEnrollment(ctx, orgID, campaignID, contactID)
		if err != nil {
			return CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: fmt.Errorf("enrollment lookup: %w", err)}
		}
		cand, acc = c, found
	}
	if acc == nil && recipientEmail != "" {
		c, found, err := s.repo.FindCandidateByEmail(ctx, orgID, strings.TrimSpace(strings.ToLower(recipientEmail)))
		if err != nil {
			return CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: fmt.Errorf("contact lookup: %w", err)}
		}
		cand, acc = c, found
		if cand != nil && (cand.DoNotContact || cand.Bounced) {
			return CampaignGateResult{Kind: GateHardBlock, Reason: ReasonDNCOrBounce}
		}
		if acc != nil && (acc.DoNotContact || acc.Blocked) {
			return CampaignGateResult{Kind: GateHardBlock, Reason: ReasonAccountDNC}
		}
	}
	// Fallback: resolve account via campaign contact id when email lookup missed.
	if acc == nil && contactID != uuid.Nil {
		if c, err := s.repo.GetCandidate(ctx, orgID, contactID); err == nil && c != nil {
			cand = c
			if a, err := s.repo.GetAccount(ctx, orgID, c.AccountID); err == nil {
				acc = a
			}
			if c.DoNotContact || c.Bounced {
				return CampaignGateResult{Kind: GateHardBlock, Reason: ReasonDNCOrBounce}
			}
			if acc != nil && (acc.DoNotContact || acc.Blocked) {
				return CampaignGateResult{Kind: GateHardBlock, Reason: ReasonAccountDNC}
			}
		}
	}
	if cand != nil && (cand.DoNotContact || cand.Bounced) {
		return CampaignGateResult{Kind: GateHardBlock, Reason: ReasonDNCOrBounce}
	}
	if acc != nil && (acc.DoNotContact || acc.Blocked) {
		return CampaignGateResult{Kind: GateHardBlock, Reason: ReasonAccountDNC}
	}
	if acc == nil {
		return CampaignGateResult{Kind: GateCommercialBlock, Reason: TargetFitReasonMissing}
	}
	if err := s.assertAuthoritativeFeedForTransport(ctx, orgID, acc); err != nil {
		return CampaignGateResult{Kind: GateCommercialBlock, Reason: "authoritative_feed_invalid", Err: err}
	}
	if cand == nil || !strings.EqualFold(strings.TrimSpace(cand.Email), strings.TrimSpace(recipientEmail)) {
		return CampaignGateResult{Kind: GateCommercialBlock, Reason: "recipient_candidate_mismatch"}
	}
	if err := RequireEmailOutbound(acc, cand); err != nil {
		reason := acc.TargetFitSuppressionReason
		if reason == "" {
			reason = ReasonTargetFit
		}
		return CampaignGateResult{Kind: GateCommercialBlock, Reason: reason, Err: err}
	}
	if enrolledTouchpoint == nil {
		return CampaignGateResult{Kind: GateCommercialBlock, Reason: "approved_touchpoint_missing"}
	}
	if err := s.AssertTransportable(ctx, orgID, enrolledTouchpoint); err != nil {
		return CampaignGateResult{Kind: GateCommercialBlock, Reason: "touchpoint_authorization_invalid", Err: err}
	}

	// ALWAYS revalidate CAMPAIGN_POLICY on open touchpoints before SMTP.
	// Independent of MessageContextHash / GeneratedContextHash (do not nest).
	if acc != nil {
		if block := s.revalidateOpenCampaignPolicyTouchpoints(ctx, orgID, acc); block != nil {
			return *block
		}
		// Material-context check (separate from policy revalidation).
		if strings.TrimSpace(acc.MessageContextHash) != "" {
			for _, st := range []string{models.TouchpointApproved, models.TouchpointQueued} {
				open, err := s.repo.ListTouchpoints(ctx, orgID, acc.ID, st, 20, 0)
				if err != nil {
					continue
				}
				for i := range open {
					tp := &open[i]
					if tp.GeneratedContextHash == "" {
						continue
					}
					if err := AssertMessageContextFresh(acc, tp.GeneratedContextHash); err != nil {
						ClearApproval(tp)
						tp.StopReason = "context_stale"
						tp.ContextStale = true
						_ = s.repo.UpdateTouchpoint(ctx, tp)
						_ = s.repo.SetAccountHumanFlags(ctx, orgID, acc.ID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueNeedsReview)
						due := time.Now().UTC().Add(time.Minute)
						return CampaignGateResult{
							Kind:     GateDeferred,
							NextSlot: due,
							Reason:   "context_stale",
							Err:      err,
						}
					}
				}
			}
		}
	}
	_ = sequenceID

	// Effective hourly cap = min(adaptive runtime, active campaign policy rate).
	capOverride := 0
	if s.policyStore != nil {
		if pol, _ := s.policyStore.GetActiveCampaignPolicy(ctx, orgID, campaignID, time.Now().UTC()); pol != nil && pol.MaxRatePerHour > 0 {
			capOverride = pol.MaxRatePerHour
		}
	}

	key := MessageKeyCampaignEmail(campaignID, contactID, sequenceID)
	res, err := s.governor.TryReserve(ctx, dispatch.ReserveRequest{
		OrganizationID: orgID,
		Channel:        dispatch.ChannelEmail,
		MessageKey:     key,
		CapOverride:    capOverride,
	})
	if err != nil {
		return CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: fmt.Errorf("dispatch governor: %w", err)}
	}
	if res.AlreadyCommitted {
		return CampaignGateResult{Kind: GateAlready, Reason: ReasonAlreadySent}
	}
	if !res.Allowed {
		due := res.NextSlot
		if due.IsZero() {
			due = time.Now().UTC().Add(s.governor.Config().MinGap)
		}
		_ = s.governor.Enqueue(ctx, dispatch.EnqueueRequest{
			OrganizationID: orgID,
			Channel:        dispatch.ChannelEmail,
			DraftID:        uuid.Nil,
			MessageKey:     key,
			RecipientRef:   strings.TrimSpace(strings.ToLower(recipientEmail)),
			DueAt:          due,
		})
		return CampaignGateResult{Kind: GateDeferred, NextSlot: due, Reason: res.Reason}
	}
	return CampaignGateResult{
		Kind:          GateProceed,
		ReservationID: res.Reservation.ID,
		Reason:        res.Reason,
	}
}

// revalidateOpenCampaignPolicyTouchpoints finds CAMPAIGN_POLICY-bound touchpoints
// for the account and checks the live grant. Independent of MessageContextHash.
// Covers unsent (APPROVED/QUEUED) and residual SENT/FAILED still bearing policy
// binding so GateCampaignEmail cannot fail open after revoke.
func (s *service) revalidateOpenCampaignPolicyTouchpoints(ctx context.Context, orgID uuid.UUID, acc *models.OutreachAccount) *CampaignGateResult {
	if acc == nil {
		return nil
	}
	all, err := s.repo.ListTouchpoints(ctx, orgID, acc.ID, "", 50, 0)
	if err != nil {
		return &CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: err}
	}
	for i := range all {
		tp := &all[i]
		if strings.TrimSpace(tp.AuthorizationMode) != AuthorizationModeCampaignPolicy {
			continue
		}
		switch tp.State {
		case models.TouchpointApproved, models.TouchpointQueued, models.TouchpointSent, models.TouchpointFailed:
		default:
			if models.TouchpointTerminalStates[tp.State] {
				continue
			}
		}
		// Grant liveness only (no CanTransport): SENT residuals must still surface revoke.
		if block := s.revalidateCampaignPolicyGrant(ctx, orgID, tp, tp.State != models.TouchpointSent && tp.State != models.TouchpointFailed); block != nil {
			return block
		}
	}
	return nil
}

// revalidateCampaignPolicyGrant loads the bound grant and fails closed if missing,
// revoked, hash-mismatched, or channel-mismatched. clearOnFail controls ClearApproval.
func (s *service) revalidateCampaignPolicyGrant(ctx context.Context, orgID uuid.UUID, tp *models.OutreachTouchpoint, clearOnFail bool) *CampaignGateResult {
	if tp == nil {
		return &CampaignGateResult{Kind: GateTransient, Reason: "nil_touchpoint", Err: fmt.Errorf("nil touchpoint")}
	}
	if strings.TrimSpace(tp.AuthorizationMode) != AuthorizationModeCampaignPolicy {
		return nil
	}
	fail := func(reason string, err error) *CampaignGateResult {
		if clearOnFail {
			ClearApproval(tp)
			tp.StopReason = reason
			_ = s.repo.UpdateTouchpoint(ctx, tp)
		}
		return &CampaignGateResult{Kind: GateDeferred, Reason: reason, NextSlot: time.Now().UTC().Add(time.Minute), Err: err}
	}
	if tp.CampaignPolicyAuthorizationID == nil || *tp.CampaignPolicyAuthorizationID == uuid.Nil {
		return fail("policy_binding_missing", nil)
	}
	if s.policyStore == nil {
		return &CampaignGateResult{Kind: GateTransient, Reason: "policy_store_missing", Err: fmt.Errorf("policy store not wired")}
	}
	auth, err := s.policyStore.GetCampaignPolicyByID(ctx, orgID, *tp.CampaignPolicyAuthorizationID)
	if err != nil {
		return &CampaignGateResult{Kind: GateTransient, Reason: ReasonGovernor, Err: err}
	}
	now := time.Now().UTC()
	if auth == nil || !auth.Active(now) {
		return fail("policy_revoked", nil)
	}
	wantHash := PolicyAuthorizationHash(auth)
	if strings.TrimSpace(tp.AuthorizationPolicyHash) == "" || tp.AuthorizationPolicyHash != wantHash {
		return fail("policy_hash_mismatch", nil)
	}
	if strings.ToUpper(strings.TrimSpace(auth.Channel)) != "" &&
		strings.ToUpper(strings.TrimSpace(auth.Channel)) != strings.ToUpper(strings.TrimSpace(tp.Channel)) {
		return fail("policy_channel_mismatch", nil)
	}
	return nil
}

// revalidateCampaignPolicyAtSend blocks transport when the bound grant is gone,
// revoked, hash-mismatched, or channel no longer matches, then checks structural
// CanTransport. Always loads the grant from the policy store.
func (s *service) revalidateCampaignPolicyAtSend(ctx context.Context, orgID uuid.UUID, tp *models.OutreachTouchpoint) *CampaignGateResult {
	if block := s.revalidateCampaignPolicyGrant(ctx, orgID, tp, true); block != nil {
		return block
	}
	if err := CanTransport(tp); err != nil {
		ClearApproval(tp)
		tp.StopReason = "transport_invalid"
		_ = s.repo.UpdateTouchpoint(ctx, tp)
		return &CampaignGateResult{Kind: GateDeferred, Reason: "transport_invalid", NextSlot: time.Now().UTC().Add(time.Minute), Err: err}
	}
	return nil
}

// AssertTransportable is the single pre-send gate for a touchpoint: structural
// CanTransport plus live CAMPAIGN_POLICY grant revalidation when applicable.
// Used by QueueTouchpoint, dispatchEmailTouch, requireTouchTransport (EnrollDraft, WhatsApp).
func (s *service) AssertTransportable(ctx context.Context, orgID uuid.UUID, tp *models.OutreachTouchpoint) error {
	if err := CanTransport(tp); err != nil {
		return err
	}
	if s.cfg.OperatorMode {
		if err := s.requirePilotMembershipForTouchpoint(ctx, orgID, tp); err != nil {
			return fmt.Errorf("controlled pilot membership: %w", err)
		}
	}
	acc, err := s.repo.GetAccount(ctx, orgID, tp.AccountID)
	if err != nil || acc == nil {
		return fmt.Errorf("target-fit account lookup failed")
	}
	if err := s.assertAuthoritativeFeedForTransport(ctx, orgID, acc); err != nil {
		return err
	}
	var cand *models.OutreachContactCandidate
	if tp.ContactCandidateID != nil {
		cand, _ = s.repo.GetCandidate(ctx, orgID, *tp.ContactCandidateID)
	} else if tp.DraftID != nil {
		if draft, _ := s.repo.GetDraft(ctx, orgID, *tp.DraftID); draft != nil && draft.ContactCandidateID != nil {
			cand, _ = s.repo.GetCandidate(ctx, orgID, *draft.ContactCandidateID)
		}
	}
	if tp.Channel == models.OutreachChannelWhatsApp {
		if err := RequireTargetFit(acc); err != nil {
			return err
		}
	} else if err := RequireEmailOutbound(acc, cand); err != nil {
		return err
	}
	if cand == nil {
		return fmt.Errorf("recipient contact candidate is missing")
	}
	if (acc.LastImportRunID != nil || cand.LastImportRunID != nil) &&
		(acc.LastImportRunID == nil || cand.LastImportRunID == nil || *acc.LastImportRunID != *cand.LastImportRunID) {
		return fmt.Errorf("recipient is not present in the current account snapshot")
	}
	if strings.TrimSpace(acc.MessageContextHash) != "" && strings.TrimSpace(tp.GeneratedContextHash) == "" {
		return fmt.Errorf("generated context hash missing")
	}
	if err := AssertMessageContextFresh(acc, tp.GeneratedContextHash); err != nil {
		return err
	}
	if tp.Channel != models.OutreachChannelWhatsApp {
		if cand == nil || !strings.EqualFold(strings.TrimSpace(tp.Recipient), strings.TrimSpace(cand.Email)) {
			return fmt.Errorf("touchpoint recipient does not match approved contact candidate")
		}
	}
	if strings.TrimSpace(tp.AuthorizationMode) != AuthorizationModeCampaignPolicy {
		return nil
	}
	if block := s.revalidateCampaignPolicyAtSend(ctx, orgID, tp); block != nil {
		return fmt.Errorf("campaign policy revalidation: %s", block.Reason)
	}
	return nil
}

func (s *service) assertAuthoritativeFeedForTransport(ctx context.Context, orgID uuid.UUID, acc *models.OutreachAccount) error {
	if s == nil || (!s.cfg.FeedSyncEnabled && !s.cfg.OperatorMode) {
		return nil
	}
	state, err := s.repo.GetFeedSyncState(ctx, orgID)
	if err != nil || state == nil {
		return fmt.Errorf("authoritative feed state unavailable")
	}
	if state.LastStatus != "completed" || state.SourceGeneratedAt == nil || state.LastSnapshotHash == "" || state.LastRunID == "" {
		return fmt.Errorf("authoritative feed is not completely applied")
	}
	maxAge := s.cfg.FeedMaxAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	now := time.Now().UTC()
	if state.SourceGeneratedAt.After(now.Add(5*time.Minute)) || now.Sub(state.SourceGeneratedAt.UTC()) > maxAge {
		return fmt.Errorf("authoritative feed is stale")
	}
	if acc == nil || strings.TrimSpace(acc.SourceRunID) != strings.TrimSpace(state.LastRunID) {
		return fmt.Errorf("account is not from the completely applied authoritative snapshot")
	}
	return nil
}

func (s *service) CompleteCampaignEmail(ctx context.Context, orgID, campaignID, contactID, sequenceID uuid.UUID, providerMessageID string) error {
	touchpoint, err := s.repo.GetTouchpointByEnrollment(ctx, orgID, campaignID, contactID)
	if err != nil {
		return err
	}
	if touchpoint == nil {
		return ErrCampaignTouchpointNotFound
	}
	commitProviderSend := func() error {
		if sequenceID == uuid.Nil || s.governor == nil {
			return fmt.Errorf("provider-confirmed CONFENGE send cannot commit its dispatch reservation")
		}
		return s.governor.CommitByMessageKey(ctx, MessageKeyCampaignEmail(campaignID, contactID, sequenceID))
	}
	if touchpoint.State == models.TouchpointSent {
		if existing := strings.TrimSpace(touchpoint.ProviderMessageID); existing != "" &&
			strings.TrimSpace(providerMessageID) != "" && existing != strings.TrimSpace(providerMessageID) {
			return fmt.Errorf("provider message id conflicts with completed touchpoint")
		}
		if err := commitProviderSend(); err != nil {
			return err
		}
		return s.releaseNextTouch(ctx, orgID, touchpoint)
	}
	// Preserve the provider-confirmed send fact even if live eligibility changed after SMTP accepted it.
	if err := CanTransport(touchpoint); err != nil {
		return err
	}
	if err := TransitionToSent(touchpoint, time.Now().UTC(), providerMessageID); err != nil {
		return err
	}
	if err := s.repo.UpdateTouchpoint(ctx, touchpoint); err != nil {
		return err
	}
	if err := commitProviderSend(); err != nil {
		return err
	}
	return s.releaseNextTouch(ctx, orgID, touchpoint)
}

// ReleaseCampaignEmail frees a lease after provider/worker publish failure.
func (s *service) ReleaseCampaignEmail(ctx context.Context, reservationID uuid.UUID, errText string) {
	if s == nil || s.governor == nil || reservationID == uuid.Nil {
		return
	}
	_ = s.governor.Release(ctx, reservationID, errText)
	_ = s.governor.RecordFailure(ctx, uuid.Nil, dispatch.ChannelEmail, "", nil, errText)
}
