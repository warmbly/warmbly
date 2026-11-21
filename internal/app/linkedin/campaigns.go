package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// Replies is the reply path an inbound LinkedIn message runs through, the same one email replies take.
type Replies interface {
	ProcessLinkedInReply(ctx context.Context, in models.LinkedInReply) *errx.Error
}

// Campaigns pulls a campaign's parked wake-up forward when a LinkedIn signal can move its contacts on.
type Campaigns interface {
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

// CampaignLogs is the campaign activity feed.
type CampaignLogs interface {
	CreateLog(ctx context.Context, entry *repository.CampaignLogEntry) error
}

// replyWindow bounds how old an inbound message may be and still run the reply path.
const replyWindow = 7 * 24 * time.Hour

// noAccountRetry is how soon a step waiting for a connected LinkedIn account is tried again.
const noAccountRetry = 15 * time.Minute

// actionTTL is how long a queued LinkedIn step may wait at GraphKit before it expires unsent.
const actionTTL = 72 * time.Hour

var stepTypes = map[string]string{
	models.ActionLinkedInInvite:   graphkit.ActionInvite,
	models.ActionLinkedInMessage:  graphkit.ActionMessage,
	models.ActionLinkedInInMail:   graphkit.ActionInmail,
	models.ActionLinkedInVisit:    graphkit.ActionProfileVisit,
	models.ActionLinkedInFollow:   graphkit.ActionFollow,
	models.ActionLinkedInLike:     graphkit.ActionLikePost,
	models.ActionLinkedInWithdraw: graphkit.ActionWithdrawInvite,
}

var actionNouns = map[string]string{
	graphkit.ActionInvite:         "LinkedIn invitation",
	graphkit.ActionMessage:        "LinkedIn message",
	graphkit.ActionInmail:         "InMail",
	graphkit.ActionProfileVisit:   "LinkedIn profile visit",
	graphkit.ActionFollow:         "LinkedIn follow",
	graphkit.ActionLikePost:       "LinkedIn like",
	graphkit.ActionWithdrawInvite: "LinkedIn invitation withdrawal",
}

var skipReasons = map[string]string{
	"already_connected":     "they were already connected",
	"invite_pending":        "an invitation to them was already pending",
	"not_connected":         "they are not connected to the account, and only connections can be messaged",
	"no_inmail_credits":     "the account has no InMail credits left",
	"no_post":               "they have no post to like",
	"opted_out":             "they do not accept messages",
	"recipient_unavailable": "their profile could not be reached",
	"no_linkedin_url":       "the contact has no LinkedIn profile",
	"no_linkedin_account":   "the campaign has no LinkedIn account to send from",
	"campaign_deleted":      "the campaign was deleted",
	"campaign_paused":       "the campaign was paused",
	"removed_from_campaign": "they were removed from the campaign",
	"unsubscribed":          "they unsubscribed",
	"suppressed":            "they are on the suppression list",
	"replied":               "they replied",
}

var errorReasons = map[string]string{
	graphkit.CodeRecipientInvalid:   "the LinkedIn profile could not be found",
	graphkit.CodeAccountUnavailable: "the LinkedIn account could not send it",
	graphkit.CodeLicenceRequired:    "the account needs a Premium or Sales Navigator licence for it",
	"empty":                         "it was empty after personalization",
	"unavailable":                   "GraphKit could not take it after several tries",
}

func plainReason(skip, code *string) string {
	if skip != nil {
		if r, ok := skipReasons[*skip]; ok {
			return r
		}
		return strings.ReplaceAll(*skip, "_", " ")
	}
	if code != nil {
		if r, ok := errorReasons[*code]; ok {
			return r
		}
		return "GraphKit refused it (" + *code + ")"
	}
	return "it did not run"
}

func strp(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// stepReference names a campaign step for one contact at GraphKit; at most 113 characters.
func stepReference(campaignID, contactID, sequenceID uuid.UUID) string {
	return fmt.Sprintf("wb:%s:%s:%s", campaignID, contactID, sequenceID)
}

// RunCampaignStep queues a due LinkedIn step at GraphKit. The action row is reserved before GraphKit is asked and
// the request carries a key derived from the step, so a retried or duplicated pass queues it once.
func (s *Service) RunCampaignStep(ctx context.Context, req models.LinkedInStepRequest) models.LinkedInStepOutcome {
	gkType, ok := stepTypes[req.Config.Type]
	if !ok {
		return models.LinkedInStepOutcome{State: models.LinkedInStepSkipped, Reason: "unknown LinkedIn step"}
	}
	ref := stepReference(req.CampaignID, req.ContactID, req.SequenceID)
	row := &models.LinkedInAction{OrganizationID: req.OrganizationID, CampaignID: &req.CampaignID, ContactID: req.ContactID,
		SequenceID: &req.SequenceID, Type: gkType, Status: models.LinkedInActionPending, Reference: ref}
	recipient, hasProfile := models.NormalizeLinkedInURL(req.LinkedInURL)
	if !hasProfile {
		now := s.now()
		row.Status, row.SkipReason, row.CompletedAt = models.LinkedInActionSkipped, strp("no_linkedin_url"), &now
	}
	held, err := s.Repo.ReserveAction(ctx, row)
	if err != nil || held == nil {
		log.Warn().Err(err).Str("campaign_id", req.CampaignID.String()).Msg("linkedin: reserving a campaign step failed")
		return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: "could not record the step"}
	}
	if held.Status != models.LinkedInActionPending || held.GraphKitID != nil {
		return settledOutcome(held)
	}
	accounts, err := s.Repo.CampaignAccounts(ctx, req.OrganizationID, req.CampaignID)
	if err != nil {
		return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: "could not read the campaign's LinkedIn accounts"}
	}
	if len(accounts) == 0 {
		return s.finish(ctx, held, models.LinkedInActionSkipped, strp("no_linkedin_account"), nil)
	}
	acct, wait, err := s.pickAccount(ctx, req, gkType, accounts)
	if err != nil {
		return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: "could not choose a LinkedIn account"}
	}
	if acct == nil {
		return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: wait, RetryAfter: noAccountRetry}
	}
	in := graphkit.ActionInput{AccountID: acct.GraphKitID, Type: gkType, Recipient: recipient, Reference: ref}
	render := func(text, part string) string {
		if req.Render == nil {
			return strings.TrimSpace(text)
		}
		return strings.TrimSpace(req.Render(text, part, acct.DisplayName))
	}
	switch gkType {
	case graphkit.ActionInvite:
		if note := cutRunes(render(req.Config.Note, "note"), models.LinkedInInviteNoteMax); note != "" {
			in.Note = &note
		}
	case graphkit.ActionMessage, graphkit.ActionInmail:
		body := render(req.Config.Body, "body")
		if body == "" {
			return s.finish(ctx, held, models.LinkedInActionFailed, nil, strp("empty"))
		}
		body = cutRunes(body, models.LinkedInMessageBodyMax)
		in.Body = &body
		if gkType == graphkit.ActionInmail {
			if subject := cutRunes(render(req.Config.Subject, "subject"), models.LinkedInInMailSubjectMax); subject != "" {
				in.Subject = &subject
			}
		}
	case graphkit.ActionLikePost:
		in.Post = strp("latest")
	}
	if err := s.ready(ctx, req.OrganizationID); err != nil {
		return s.attemptFailed(ctx, held, err)
	}
	// Nothing stale goes out: GraphKit drops the action if it has not run in actionTTL.
	expires := s.now().Add(actionTTL)
	in.ExpiresAt = &expires
	var act *graphkit.Action
	if held.Attempts > 0 || s.now().Sub(held.CreatedAt) > time.Minute {
		// GraphKit forgets an Idempotency-Key after a day, so a step tried before is looked up first.
		if act, err = s.actionByReference(ctx, req.OrganizationID, ref); err != nil {
			return s.attemptFailed(ctx, held, err)
		}
	}
	if act == nil {
		key := "wb:li:" + strings.TrimPrefix(ref, "wb:")
		act, err = s.api().CreateAction(ctx, ext(req.OrganizationID), in, key)
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeIdempotencyConflict {
			if act, err = s.actionByReference(ctx, req.OrganizationID, ref); err == nil && act == nil {
				// The key is held by a request GraphKit has not finished; the step is read back next time.
				return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: "GraphKit is still taking the step", RetryAfter: linkedInBackoff(held.Attempts)}
			}
		}
		if err != nil {
			if ge, ok := graphkit.AsError(err); ok && !ge.Retryable() && ge.Status >= 400 && ge.Status < 500 {
				return s.finish(ctx, held, models.LinkedInActionFailed, nil, strp(ge.Code))
			}
			return s.attemptFailed(ctx, held, err)
		}
	}
	if err := s.Repo.SetActionQueued(ctx, req.OrganizationID, held.ID, acct.ID, act.ID, actionStatus(act.Status)); err != nil {
		// GraphKit holds the action under the step's key; the next pass reads it back.
		return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: "could not record the queued step"}
	}
	if act.Status != graphkit.ActionQueued && act.Status != graphkit.ActionRunning {
		if err := s.applyAction(ctx, req.OrganizationID, act); err != nil {
			log.Warn().Err(err).Str("action_id", act.ID).Msg("linkedin: applying a finished action failed")
		}
	}
	return models.LinkedInStepOutcome{State: models.LinkedInStepQueued, Reason: actionNouns[gkType] + " queued from " + acct.DisplayName}
}

// settledOutcome is what a step whose action row already moved past pending means for routing.
func settledOutcome(a *models.LinkedInAction) models.LinkedInStepOutcome {
	switch a.Status {
	case models.LinkedInActionSkipped:
		return models.LinkedInStepOutcome{State: models.LinkedInStepSkipped, Reason: plainReason(a.SkipReason, nil), Code: deref(a.SkipReason)}
	case models.LinkedInActionFailed, models.LinkedInActionCancelled, models.LinkedInActionExpired:
		return models.LinkedInStepOutcome{State: models.LinkedInStepFailed, Reason: plainReason(nil, a.ErrorCode), Code: deref(a.ErrorCode)}
	}
	return models.LinkedInStepOutcome{State: models.LinkedInStepQueued, Reason: actionNouns[a.Type] + " queued"}
}

func (s *Service) finish(ctx context.Context, a *models.LinkedInAction, status string, skip, code *string) models.LinkedInStepOutcome {
	if err := s.Repo.FinishAction(ctx, a.OrganizationID, a.ID, status, skip, code); err != nil {
		return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: "could not record the step"}
	}
	a.Status, a.SkipReason, a.ErrorCode = status, skip, code
	return settledOutcome(a)
}

// attemptFailed counts a try GraphKit may take later and backs off. A refusal fails the step at the attempt cap;
// an outage or a network failure only once the step is older than an action may live, so it never burns the cap.
func (s *Service) attemptFailed(ctx context.Context, a *models.LinkedInAction, cause error) models.LinkedInStepOutcome {
	n, err := s.Repo.CountActionAttempt(ctx, a.OrganizationID, a.ID)
	if err != nil {
		n = a.Attempts + 1
	}
	ge, refused := graphkit.AsError(cause)
	refused = (refused && ge.Status >= 400 && ge.Status < 500) || errors.Is(cause, errUnavailable)
	if err == nil && n >= config.CampaignSendMaxAttempts && (refused || s.now().Sub(a.CreatedAt) > actionTTL) {
		return s.finish(ctx, a, models.LinkedInActionFailed, nil, strp("unavailable"))
	}
	reason := "GraphKit did not take the step; trying again"
	if ge != nil && ge.Code != "" {
		reason += " (" + ge.Code + ")"
	}
	return models.LinkedInStepOutcome{State: models.LinkedInStepRetry, Reason: reason, RetryAfter: linkedInBackoff(n)}
}

// linkedInBackoff is how long a contact's step waits after n failed tries: two minutes, doubling, at most an hour.
func linkedInBackoff(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	if n > 5 {
		return time.Hour
	}
	return min(2*time.Minute<<n, time.Hour)
}

// actionByReference is the action GraphKit holds for the step, nil when it holds none.
func (s *Service) actionByReference(ctx context.Context, orgID uuid.UUID, ref string) (*graphkit.Action, error) {
	page, err := s.api().ListActions(ctx, ext(orgID), graphkit.ActionFilter{Reference: ref, Limit: 1})
	if err != nil || len(page.Data) == 0 {
		return nil, err
	}
	return &page.Data[0], nil
}

// pickAccount keeps a contact on the account used before, or one connected to them; new contacts go to the account
// with the most of today's limit left for the action. wait says why none can take it right now.
func (s *Service) pickAccount(ctx context.Context, req models.LinkedInStepRequest, gkType string, accounts []models.LinkedInAccount) (*models.LinkedInAccount, string, error) {
	ids := make([]uuid.UUID, len(accounts))
	byID := map[uuid.UUID]*models.LinkedInAccount{}
	for i := range accounts {
		ids[i] = accounts[i].ID
		byID[accounts[i].ID] = &accounts[i]
	}
	sticky, err := s.Repo.StickyAccount(ctx, req.OrganizationID, req.CampaignID, req.ContactID, ids)
	if err != nil {
		return nil, "", err
	}
	if sticky != nil {
		a := byID[*sticky]
		if !sendable(a) {
			return nil, "waiting for " + a.DisplayName + ", the LinkedIn account this contact is with, to be connected again", nil
		}
		return a, "", nil
	}
	var live []*models.LinkedInAccount
	for i := range accounts {
		if sendable(&accounts[i]) {
			live = append(live, &accounts[i])
		}
	}
	if len(live) == 0 {
		return nil, "waiting for one of the campaign's LinkedIn accounts to be connected again", nil
	}
	open, err := s.Repo.OpenActionsToday(ctx, req.OrganizationID, ids, s.now().Truncate(24*time.Hour))
	if err != nil {
		return nil, "", err
	}
	left := func(a *models.LinkedInAccount) int {
		limit, used := 0, 0
		var snap graphkit.SocialAccount
		if json.Unmarshal(a.Snapshot, &snap) == nil {
			for _, u := range snap.Usage {
				if u.Action == gkType {
					limit, used = u.DailyLimit, u.Today
				}
			}
		}
		return limit - used - open[a.ID][gkType]
	}
	sort.SliceStable(live, func(i, j int) bool { return left(live[i]) > left(live[j]) })
	return live[0], "", nil
}

func sendable(a *models.LinkedInAccount) bool {
	return a != nil && a.RemovedAt == nil && !a.UnpaidPaused && a.Status == graphkit.SocialActive
}

// cutRunes shortens text to max runes, at a word boundary when one is close.
func cutRunes(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	r := []rune(text)[:max]
	for i := len(r) - 1; i > max*3/4; i-- {
		if unicode.IsSpace(r[i]) {
			return strings.TrimRightFunc(string(r[:i]), unicode.IsSpace)
		}
	}
	return string(r)
}

func actionStatus(gk string) string {
	switch gk {
	case graphkit.ActionQueued, graphkit.ActionRunning, graphkit.ActionSucceeded, graphkit.ActionSkipped,
		graphkit.ActionFailed, graphkit.ActionCancelled, graphkit.ActionExpired:
		return gk
	}
	return models.LinkedInActionQueued
}

// ---- GraphKit signals -----------------------------------------------------

// applyAction records social.action.updated for an action a campaign step queued.
func (s *Service) applyAction(ctx context.Context, orgID uuid.UUID, a *graphkit.Action) error {
	if a == nil || a.ID == "" {
		return nil
	}
	u := repository.LinkedInActionUpdate{GraphKitID: a.ID, Status: actionStatus(a.Status), SkipReason: a.SkipReason, CompletedAt: a.CompletedAt}
	if a.Reference != nil {
		u.Reference = *a.Reference
	}
	if a.Error != nil && a.Error.Code != "" {
		u.ErrorCode = &a.Error.Code
	}
	row, changed, err := s.Repo.ApplyActionUpdate(ctx, orgID, u)
	if err != nil || row == nil || !changed {
		return err
	}
	at := s.now()
	if a.CompletedAt != nil {
		at = a.CompletedAt.UTC()
	}
	var change *repository.LinkedInStateChange
	pending, notPending := true, false
	switch row.Status {
	case models.LinkedInActionSucceeded:
		switch row.Type {
		case graphkit.ActionInvite:
			change = &repository.LinkedInStateChange{InviteSentAt: &at, InvitePending: &pending}
		case graphkit.ActionMessage, graphkit.ActionInmail:
			change = &repository.LinkedInStateChange{MessageAt: &at}
		case graphkit.ActionWithdrawInvite:
			change = &repository.LinkedInStateChange{InvitePending: &notPending}
		}
	case models.LinkedInActionSkipped:
		switch {
		case a.SkipReason != nil && *a.SkipReason == "already_connected":
			change = &repository.LinkedInStateChange{ConnectedAt: &at}
		case a.SkipReason != nil && *a.SkipReason == "invite_pending":
			change = &repository.LinkedInStateChange{InvitePending: &pending}
		}
	}
	if change != nil && row.AccountID != nil {
		if err := s.Repo.NoteContactState(ctx, orgID, []uuid.UUID{row.ContactID}, *row.AccountID, *change); err != nil {
			return err
		}
		if row.CampaignID != nil {
			s.wake(ctx, orgID, []string{row.CampaignID.String()})
		}
	}
	if row.CampaignID != nil && (row.Status == models.LinkedInActionSkipped || row.Status == models.LinkedInActionFailed ||
		row.Status == models.LinkedInActionExpired) {
		s.logStep(ctx, row, a.Recipient.FullName)
	}
	ev := pubsub.LinkedInEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventLinkedInActionUpdated}, Status: row.Status,
		ContactID: row.ContactID.String(), ActionType: row.Type}
	if row.AccountID != nil {
		ev.AccountID = row.AccountID.String()
	}
	if row.CampaignID != nil {
		ev.CampaignID = row.CampaignID.String()
	}
	s.publish(ctx, orgID, ev)
	return nil
}

// stopBatch bounds how many queued steps one cancellation pass takes back.
const stopBatch = 100

// stopActions cancels at GraphKit the queued campaign steps whose contact should no longer get them;
// contactID narrows the pass to one contact.
func (s *Service) stopActions(ctx context.Context, contactID *uuid.UUID) error {
	if !s.available() {
		return nil
	}
	rows, err := s.Repo.ActionsToStop(ctx, contactID, stopBatch)
	if err != nil {
		return err
	}
	for i := range rows {
		r := &rows[i]
		_, err := s.api().CancelAction(ctx, ext(r.OrganizationID), r.GraphKitID, "linkedin:cancel:"+r.ID.String())
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeStateConflict {
			// It ran or is running: record what GraphKit says rather than ask again every pass.
			if act, gerr := s.api().GetAction(ctx, ext(r.OrganizationID), r.GraphKitID); gerr == nil {
				if aerr := s.applyAction(ctx, r.OrganizationID, act); aerr != nil {
					log.Warn().Err(aerr).Str("action_id", r.GraphKitID).Msg("linkedin: applying an action that could not be cancelled failed")
				}
			}
			continue
		}
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeNotFound {
			err = nil
		}
		if err != nil {
			log.Warn().Err(err).Str("action_id", r.GraphKitID).Msg("linkedin: cancelling a queued step failed; retried on the next pass")
			continue
		}
		if err := s.Repo.FinishAction(ctx, r.OrganizationID, r.ID, models.LinkedInActionCancelled, strp(r.Reason), nil); err != nil {
			return err
		}
		row := &models.LinkedInAction{ID: r.ID, OrganizationID: r.OrganizationID, CampaignID: r.CampaignID, ContactID: r.ContactID,
			Type: r.Type, Status: models.LinkedInActionCancelled, SkipReason: strp(r.Reason)}
		s.logStep(ctx, row, "")
		ev := pubsub.LinkedInEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventLinkedInActionUpdated}, Status: row.Status,
			ContactID: row.ContactID.String(), ActionType: row.Type}
		if row.CampaignID != nil {
			ev.CampaignID = row.CampaignID.String()
		}
		s.publish(ctx, r.OrganizationID, ev)
	}
	return nil
}

func (s *Service) logStep(ctx context.Context, row *models.LinkedInAction, name string) {
	if s.CampaignLogs == nil || row.CampaignID == nil {
		return
	}
	who := strings.TrimSpace(name)
	if who == "" {
		who = "a contact"
	}
	verb, event := "was skipped", "linkedin_skipped"
	switch row.Status {
	case models.LinkedInActionFailed:
		verb, event = "failed", "linkedin_failed"
	case models.LinkedInActionExpired:
		verb, event = "expired before it ran", "linkedin_failed"
	case models.LinkedInActionCancelled:
		verb, event = "was cancelled before it ran", "linkedin_cancelled"
	}
	reason := plainReason(row.SkipReason, row.ErrorCode)
	_ = s.CampaignLogs.CreateLog(ctx, &repository.CampaignLogEntry{CampaignID: *row.CampaignID, EventType: event,
		Message: fmt.Sprintf("%s to %s %s: %s", actionNouns[row.Type], who, verb, reason),
		Metadata: map[string]interface{}{"contact_id": row.ContactID.String(), "action_type": row.Type, "status": row.Status,
			"skip_reason": row.SkipReason, "error_code": row.ErrorCode}})
}

// applyInvitationAccepted records a new connection and wakes the campaigns waiting on it.
func (s *Service) applyInvitationAccepted(ctx context.Context, orgID uuid.UUID, ia *graphkit.InvitationAccepted) error {
	acct, err := s.Repo.GetAccountByGraphKitID(ctx, orgID, ia.AccountID)
	if err != nil || acct == nil {
		return err
	}
	contacts, err := s.Repo.ContactIDsByPublicID(ctx, orgID, strings.ToLower(ia.Person.PublicID))
	if err != nil || len(contacts) == 0 {
		return err
	}
	at := ia.ConnectedAt.UTC()
	if at.IsZero() {
		at = s.now()
	}
	if err := s.Repo.NoteContactState(ctx, orgID, contacts, acct.ID, repository.LinkedInStateChange{ConnectedAt: &at}); err != nil {
		return err
	}
	campaigns, err := s.Repo.ActiveCampaignsFor(ctx, orgID, contacts)
	if err != nil {
		return err
	}
	s.wake(ctx, orgID, campaigns)
	for _, c := range contacts {
		s.publish(ctx, orgID, pubsub.LinkedInEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventLinkedInActionUpdated},
			AccountID: acct.ID.String(), ContactID: c.String(), Status: "invitation_accepted", ActionType: graphkit.ActionInvite})
	}
	return nil
}

func (s *Service) wake(ctx context.Context, orgID uuid.UUID, campaignIDs []string) {
	if s.Campaigns != nil && len(campaignIDs) > 0 {
		s.Campaigns.WakeCampaigns(context.WithoutCancel(ctx), orgID, campaignIDs)
	}
}

// noteMessage folds a stored message into the contact's LinkedIn state, and runs a new inbound one through the reply path.
func (s *Service) noteMessage(ctx context.Context, conv *models.LinkedInConversation, acct *models.LinkedInAccount, m *graphkit.SocialMessage) error {
	at := s.now()
	if m.SentAt != nil {
		at = m.SentAt.UTC()
	}
	inbound := m.Direction == graphkit.DirectionInbound
	if conv.ContactID != nil {
		change := repository.LinkedInStateChange{MessageAt: &at}
		if inbound {
			change.RepliedAt = &at
		}
		if err := s.Repo.NoteContactState(ctx, conv.OrganizationID, []uuid.UUID{*conv.ContactID}, conv.AccountID, change); err != nil {
			return err
		}
	}
	if !inbound || s.now().Sub(at) > replyWindow {
		return nil
	}
	id, err := s.Repo.MessageID(ctx, conv.OrganizationID, m.ID)
	if err != nil || id == uuid.Nil {
		return err
	}
	claimed, err := s.Repo.ClaimInboundReply(ctx, conv.OrganizationID, id)
	if err != nil || !claimed {
		return err
	}
	var participant graphkit.Participant
	_ = json.Unmarshal(conv.Participant, &participant)
	in := models.LinkedInReply{OrganizationID: conv.OrganizationID, ConversationID: conv.ID, AccountID: conv.AccountID,
		ContactID: conv.ContactID, Sender: firstNonEmpty(participant.FullName, participant.PublicID, "a LinkedIn contact"), Body: m.Body}
	if acct != nil {
		in.NotifyUserID = acct.UserID
	}
	if conv.ContactID != nil {
		campaignID, sequenceID, err := s.Repo.ReplyCampaign(ctx, conv.OrganizationID, *conv.ContactID, at)
		if err != nil {
			return err
		}
		if campaignID != nil {
			in.CampaignID, in.SequenceID = campaignID, sequenceID
		}
		campaigns, err := s.Repo.ActiveCampaignsFor(ctx, conv.OrganizationID, []uuid.UUID{*conv.ContactID})
		if err == nil {
			defer s.wake(ctx, conv.OrganizationID, campaigns)
		}
	}
	if s.Replies != nil {
		if xerr := s.Replies.ProcessLinkedInReply(context.WithoutCancel(ctx), in); xerr != nil {
			log.Warn().Err(xerr).Str("conversation_id", conv.ID.String()).Msg("linkedin: the reply path failed for an inbound message")
		}
	}
	if conv.ContactID != nil {
		// The reply stops the contact's queued steps now; the reconcile pass catches a cancel that fails here.
		if err := s.stopActions(context.WithoutCancel(ctx), conv.ContactID); err != nil {
			log.Warn().Err(err).Str("conversation_id", conv.ID.String()).Msg("linkedin: stopping a replying contact's queued steps failed")
		}
	}
	return nil
}

// ---- campaign senders, activity, stats ------------------------------------

// CampaignAccountIDs are the LinkedIn accounts a campaign sends its LinkedIn steps from.
func (s *Service) CampaignAccountIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, error) {
	return s.Repo.CampaignAccountIDs(ctx, orgID, campaignID)
}

// SetCampaignAccounts replaces them.
func (s *Service) SetCampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) > maxLimit {
		return nil, invalid("Pick at most 100 LinkedIn accounts.")
	}
	seen := map[uuid.UUID]bool{}
	uniq := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	ok, err := s.Repo.SetCampaignAccounts(ctx, orgID, campaignID, uniq)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errx.NewWithIdentifier(errx.NotFound, "not_found", "The campaign, or one of the LinkedIn accounts, was not found.")
	}
	return s.Repo.CampaignAccountIDs(ctx, orgID, campaignID)
}

// ContactActivity is GET /v1/linkedin/contacts/:id/activity.
type ContactActivity struct {
	Actions       []models.LinkedInAction `json:"actions"`
	Conversations []*Conversation         `json:"conversations"`
}

// ContactActivity is a contact's LinkedIn steps and conversations, newest first.
func (s *Service) ContactActivity(ctx context.Context, orgID, contactID uuid.UUID) (*ContactActivity, error) {
	actions, err := s.Repo.ContactActions(ctx, orgID, contactID, maxLimit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListConversations(ctx, orgID, repository.LinkedInConversationFilter{ContactID: &contactID, Limit: maxLimit})
	if err != nil {
		return nil, err
	}
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := &ContactActivity{Actions: actions, Conversations: make([]*Conversation, 0, len(rows))}
	for i := range rows {
		out.Conversations = append(out.Conversations, s.toConversation(ctx, ci, &rows[i]))
	}
	return out, nil
}

// LeadsWithoutProfile is how many of a campaign's leads skip its LinkedIn steps for want of a profile.
func (s *Service) LeadsWithoutProfile(ctx context.Context, orgID, campaignID uuid.UUID) (int, error) {
	return s.Repo.LeadsWithoutProfile(ctx, orgID, campaignID)
}

// CampaignStats is the LinkedIn block of each campaign's analytics; a campaign with no LinkedIn step reads zero.
func (s *Service) CampaignStats(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID) (map[uuid.UUID]*models.LinkedInCampaignStats, error) {
	return s.Repo.CampaignStats(ctx, orgID, campaignIDs)
}

// UnreadConversations counts the workspace's LinkedIn conversations with unread messages.
func (s *Service) UnreadConversations(ctx context.Context, orgID uuid.UUID) (int, error) {
	return s.Repo.UnreadConversations(ctx, orgID)
}
