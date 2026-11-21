package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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

// Replies is the reply path an inbound social message runs through, the same one email replies take.
type Replies interface {
	ProcessSocialReply(ctx context.Context, in models.SocialReply) *errx.Error
}

// Campaigns pulls a campaign's parked wake-up forward when a social signal can move its contacts on.
type Campaigns interface {
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

// CampaignLogs is the campaign activity feed.
type CampaignLogs interface {
	CreateLog(ctx context.Context, entry *repository.CampaignLogEntry) error
}

// replyWindow bounds how old an inbound message may be and still run the reply path.
const replyWindow = 7 * 24 * time.Hour

// noAccountRetry is how soon a step waiting for a connected account is tried again.
const noAccountRetry = 15 * time.Minute

// unknownStepRetry is how long a contact is held on a step this release cannot run before it is offered again.
const unknownStepRetry = time.Hour

// actionTTL is how long a queued step may wait at GraphKit before it expires unsent.
const actionTTL = 72 * time.Hour

// Skip reasons Warmbly records itself, before GraphKit is asked.
const (
	skipNoHandle       = "no_handle"
	skipConsentMissing = "consent_missing"
	skipNoAccount      = "no_account"
)

var actionNounsByType = map[string]string{
	graphkit.ActionInvite:         "invitation",
	graphkit.ActionMessage:        "message",
	graphkit.ActionInmail:         "InMail",
	graphkit.ActionVoiceNote:      "voice note",
	graphkit.ActionProfileVisit:   "profile visit",
	graphkit.ActionFollow:         "follow",
	graphkit.ActionLikePost:       "like",
	graphkit.ActionCommentPost:    "comment",
	graphkit.ActionEndorse:        "endorsement",
	graphkit.ActionWithdrawInvite: "invitation withdrawal",
}

// actionNoun names a step for the campaign feed, such as "WhatsApp voice note".
func actionNoun(channel, typ string) string {
	noun, ok := actionNounsByType[typ]
	if !ok {
		noun = strings.ReplaceAll(typ, "_", " ")
	}
	if typ == graphkit.ActionInmail {
		return noun
	}
	return models.SocialChannelLabel(channelOr(channel)) + " " + noun
}

func channelOr(channel string) string {
	if channel == "" {
		return models.SocialLinkedIn
	}
	return channel
}

var skipReasons = map[string]string{
	"already_connected":     "they were already connected",
	"invite_pending":        "an invitation to them was already pending",
	"not_connected":         "they are not connected to the account, and only connections can be messaged",
	"no_inmail_credits":     "the account has no InMail credits left",
	"no_recent_post":        "they have no recent post",
	"already_commented":     "the account already commented on their latest post",
	"no_skills":             "they have no skills to endorse",
	"no_pending_invite":     "no invitation to them was pending",
	"opted_out":             "they do not accept messages",
	"recipient_unavailable": "their profile could not be reached",
	skipConsentMissing:      "there is no consent basis to message them on that channel",
	skipNoHandle:            "the contact has no handle on that channel",
	skipNoAccount:           "the campaign has no account on that channel to send from",
	"campaign_deleted":      "the campaign was deleted",
	"campaign_paused":       "the campaign was paused",
	"removed_from_campaign": "they were removed from the campaign",
	"unsubscribed":          "they unsubscribed",
	"suppressed":            "they are on the suppression list",
	"replied":               "they replied",
}

var errorReasons = map[string]string{
	graphkit.CodeRecipientInvalid:      "the profile could not be found",
	graphkit.CodeAccountUnavailable:    "the account could not send it",
	graphkit.CodeLicenceRequired:       "the account needs a Premium or Sales Navigator licence for it",
	graphkit.CodeCapabilityUnavailable: "the account cannot run this action",
	graphkit.CodeConsentRequired:       "the contact has no consent basis on that channel",
	"empty":                            "it was empty after personalization",
	"unavailable":                      "GraphKit could not take it after several tries",
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

// RunCampaignStep queues a due social step at GraphKit. The action row is reserved before GraphKit is asked and the
// request carries a key derived from the step, so a retried or duplicated pass queues it once.
func (s *Service) RunCampaignStep(ctx context.Context, req models.SocialStepRequest) models.SocialStepOutcome {
	step := req.Step
	if !models.SocialActionOn(step.Channel, step.Action) {
		// A channel or action a newer release added: hold the contact rather than pass the step by.
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "this release cannot run the step yet", RetryAfter: unknownStepRetry}
	}
	ref := stepReference(req.CampaignID, req.ContactID, req.SequenceID)
	row := &models.SocialActionRow{OrganizationID: req.OrganizationID, CampaignID: &req.CampaignID, ContactID: req.ContactID,
		SequenceID: &req.SequenceID, Channel: step.Channel, Type: step.Action, Status: models.SocialActionPending, Reference: ref}
	ident, err := s.Repo.ContactIdentity(ctx, req.OrganizationID, req.ContactID, step.Channel)
	if err != nil {
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "could not read the contact's handle"}
	}
	basis := ""
	if ident != nil && ident.ConsentBasis != nil {
		basis = *ident.ConsentBasis
	}
	spec, _ := s.channelSpec(ctx, step.Channel)
	switch {
	case ident == nil || ident.Normalized == "":
		row.SkipReason = strp(skipNoHandle)
	case spec.ConsentRequired && !offers(spec.ConsentBases, basis):
		row.SkipReason = strp(skipConsentMissing)
	}
	if row.SkipReason != nil {
		now := s.now()
		row.Status, row.CompletedAt = models.SocialActionSkipped, &now
	}
	held, err := s.Repo.ReserveAction(ctx, row)
	if err != nil || held == nil {
		log.Warn().Err(err).Str("campaign_id", req.CampaignID.String()).Msg("social: reserving a campaign step failed")
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "could not record the step"}
	}
	if held.Status != models.SocialActionPending || held.GraphKitID != nil {
		return settledOutcome(held, &step)
	}
	accounts, err := s.channelAccounts(ctx, req.OrganizationID, req.CampaignID, step.Channel)
	if err != nil {
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "could not read the campaign's social accounts"}
	}
	if len(accounts) == 0 {
		return s.finish(ctx, held, &step, models.SocialActionSkipped, strp(skipNoAccount), nil)
	}
	acct, wait, err := s.pickAccount(ctx, req, accounts)
	if err != nil {
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "could not choose an account"}
	}
	if acct == nil {
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: wait, RetryAfter: noAccountRetry}
	}
	in, empty := s.stepInput(req, spec, acct, models.SocialRecipient(step.Channel, ident.Normalized), ref)
	if empty {
		return s.finish(ctx, held, &step, models.SocialActionFailed, nil, strp("empty"))
	}
	if spec.ConsentRequired {
		in.Consent = &graphkit.ConsentInput{Basis: basis}
	}
	if err := s.ready(ctx, req.OrganizationID); err != nil {
		return s.attemptFailed(ctx, held, &step, err)
	}
	// Nothing stale goes out: GraphKit drops the action if it has not run in actionTTL.
	expires := s.now().Add(actionTTL)
	in.ExpiresAt = &expires
	var act *graphkit.Action
	if held.Attempts > 0 || s.now().Sub(held.CreatedAt) > time.Minute {
		// GraphKit forgets an Idempotency-Key after a day, so a step tried before is looked up first.
		if act, err = s.actionByReference(ctx, req.OrganizationID, ref); err != nil {
			return s.attemptFailed(ctx, held, &step, err)
		}
	}
	if act == nil {
		key := "wb:so:" + strings.TrimPrefix(ref, "wb:")
		act, err = s.api().CreateAction(ctx, ext(req.OrganizationID), in, key)
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeIdempotencyConflict {
			if act, err = s.actionByReference(ctx, req.OrganizationID, ref); err == nil && act == nil {
				// The key is held by a request GraphKit has not finished; the step is read back next time.
				return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "GraphKit is still taking the step", RetryAfter: socialBackoff(held.Attempts)}
			}
		}
		if err != nil {
			if ge, ok := graphkit.AsError(err); ok && !ge.Retryable() && ge.Status >= 400 && ge.Status < 500 {
				return s.finish(ctx, held, &step, models.SocialActionFailed, nil, strp(ge.Code))
			}
			return s.attemptFailed(ctx, held, &step, err)
		}
	}
	if err := s.Repo.SetActionQueued(ctx, req.OrganizationID, held.ID, acct.ID, act.ID, actionStatus(act.Status)); err != nil {
		// GraphKit holds the action under the step's key; the next pass reads it back.
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "could not record the queued step"}
	}
	if act.Status != graphkit.ActionQueued && act.Status != graphkit.ActionRunning {
		if err := s.applyAction(ctx, req.OrganizationID, act); err != nil {
			log.Warn().Err(err).Str("action_id", act.ID).Msg("social: applying a finished action failed")
		}
	}
	return models.SocialStepOutcome{State: models.SocialStepQueued, Reason: actionNoun(step.Channel, step.Action) + " queued from " + acct.DisplayName}
}

// channelAccounts are the campaign's senders on one channel.
func (s *Service) channelAccounts(ctx context.Context, orgID, campaignID uuid.UUID, channel string) ([]models.SocialAccount, error) {
	all, err := s.Repo.CampaignAccounts(ctx, orgID, campaignID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, a := range all {
		if channelOr(a.Channel) == channel {
			out = append(out, a)
		}
	}
	return out, nil
}

// stepInput is the GraphKit action a step becomes for one contact; empty is true when its copy rendered to nothing.
func (s *Service) stepInput(req models.SocialStepRequest, spec graphkit.SocialChannel, acct *models.SocialAccount, recipient, ref string) (graphkit.ActionInput, bool) {
	step := req.Step
	in := graphkit.ActionInput{AccountID: acct.GraphKitID, Type: step.Action, Recipient: recipient, Reference: ref}
	render := func(text, fallback, part string) string {
		if req.Render == nil {
			return strings.TrimSpace(text)
		}
		out, missing := req.Render(text, part, acct.DisplayName)
		if missing && strings.TrimSpace(fallback) != "" {
			out, _ = req.Render(fallback, part, acct.DisplayName)
		}
		return strings.TrimSpace(out)
	}
	cp := step.CopyFor(req.ContactID, req.SequenceID)
	switch step.Action {
	case graphkit.ActionInvite:
		if note := cutRunes(render(cp.Note, step.FallbackNote, "note"), spec.InviteNoteMaxChars); note != "" {
			in.Note = &note
		}
	case graphkit.ActionMessage, graphkit.ActionInmail:
		limit := spec.MessageMaxChars
		if step.Action == graphkit.ActionInmail {
			limit = spec.InmailBodyMax
			if subject := cutRunes(render(cp.Subject, step.FallbackSubject, "subject"), spec.InmailSubjectMax); subject != "" {
				in.Subject = &subject
			}
		}
		body := cutRunes(render(cp.Body, step.FallbackBody, "body"), limit)
		if body == "" {
			return in, true
		}
		in.Body = &body
	case graphkit.ActionVoiceNote:
		v := step.Voice
		if v == nil {
			return in, true
		}
		if v.Mode == models.SocialVoiceRecording {
			if v.AudioID == "" {
				return in, true
			}
			in.Voice = &graphkit.VoiceInput{Mode: v.Mode, AudioID: v.AudioID}
			break
		}
		script := cutRunes(render(v.Script, v.FallbackScript, "script"), spec.VoiceScriptMaxChars)
		if script == "" {
			return in, true
		}
		in.Voice = &graphkit.VoiceInput{Mode: models.SocialVoiceWritten, Script: script}
	case graphkit.ActionLikePost, graphkit.ActionCommentPost:
		in.Post = strp("latest")
		if step.PostAgeDays > 0 {
			days := min(step.PostAgeDays, max(spec.MaxPostAgeDays, 1))
			in.MaxPostAgeDays = &days
		}
		if step.Action == graphkit.ActionLikePost {
			if step.Reaction != "" && offers(spec.Reactions, step.Reaction) {
				in.Reaction = strp(step.Reaction)
			}
			break
		}
		comment := cutRunes(render(pickComment(step.Comments, req.ContactID, req.SequenceID), "", "comment"), spec.CommentMaxChars)
		if comment == "" {
			return in, true
		}
		in.Comment = &comment
	case graphkit.ActionEndorse:
		if step.MaxSkills > 0 {
			n := step.MaxSkills
			in.MaxSkills = &n
		}
	}
	return in, false
}

// pickComment is one of a comment step's comments, the same one for a contact on every pass.
func pickComment(comments []string, contactID, stepID uuid.UUID) string {
	var live []string
	for _, c := range comments {
		if strings.TrimSpace(c) != "" {
			live = append(live, c)
		}
	}
	if len(live) == 0 {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write(contactID[:])
	_, _ = h.Write(stepID[:])
	return live[h.Sum64()%uint64(len(live))]
}

// missingSkip reports a skip that means the contact cannot be reached on the step's channel.
func missingSkip(reason *string) bool {
	return reason != nil && (*reason == skipNoHandle || *reason == skipConsentMissing)
}

// settledOutcome is what a step whose action row already moved past pending means for routing.
func settledOutcome(a *models.SocialActionRow, step *models.SocialStep) models.SocialStepOutcome {
	switch a.Status {
	case models.SocialActionSkipped:
		out := models.SocialStepOutcome{State: models.SocialStepSkipped, Reason: plainReason(a.SkipReason, nil), Code: deref(a.SkipReason)}
		if step != nil && step.StopsWhenMissing() && missingSkip(a.SkipReason) {
			out.State = models.SocialStepStop
		}
		return out
	case models.SocialActionFailed, models.SocialActionCancelled, models.SocialActionExpired:
		return models.SocialStepOutcome{State: models.SocialStepFailed, Reason: plainReason(nil, a.ErrorCode), Code: deref(a.ErrorCode)}
	}
	return models.SocialStepOutcome{State: models.SocialStepQueued, Reason: actionNoun(a.Channel, a.Type) + " queued"}
}

func (s *Service) finish(ctx context.Context, a *models.SocialActionRow, step *models.SocialStep, status string, skip, code *string) models.SocialStepOutcome {
	if err := s.Repo.FinishAction(ctx, a.OrganizationID, a.ID, status, skip, code); err != nil {
		return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: "could not record the step"}
	}
	a.Status, a.SkipReason, a.ErrorCode = status, skip, code
	return settledOutcome(a, step)
}

// attemptFailed counts a try GraphKit may take later and backs off. A refusal fails the step at the attempt cap;
// an outage or a network failure only once the step is older than an action may live, so it never burns the cap.
func (s *Service) attemptFailed(ctx context.Context, a *models.SocialActionRow, step *models.SocialStep, cause error) models.SocialStepOutcome {
	n, err := s.Repo.CountActionAttempt(ctx, a.OrganizationID, a.ID)
	if err != nil {
		n = a.Attempts + 1
	}
	ge, refused := graphkit.AsError(cause)
	refused = (refused && ge.Status >= 400 && ge.Status < 500) || errors.Is(cause, errUnavailable)
	if err == nil && n >= config.CampaignSendMaxAttempts && (refused || s.now().Sub(a.CreatedAt) > actionTTL) {
		return s.finish(ctx, a, step, models.SocialActionFailed, nil, strp("unavailable"))
	}
	reason := "GraphKit did not take the step; trying again"
	if ge != nil && ge.Code != "" {
		reason += " (" + ge.Code + ")"
	}
	return models.SocialStepOutcome{State: models.SocialStepRetry, Reason: reason, RetryAfter: socialBackoff(n)}
}

// socialBackoff is how long a contact's step waits after n failed tries: two minutes, doubling, at most an hour.
func socialBackoff(n int) time.Duration {
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

// pickAccount keeps a contact on the account bound to them, or one connected to them; new contacts go to the account
// with the most of today's limit left for the action. wait says why none can take it right now.
func (s *Service) pickAccount(ctx context.Context, req models.SocialStepRequest, accounts []models.SocialAccount) (*models.SocialAccount, string, error) {
	label := models.SocialChannelLabel(req.Step.Channel)
	ids := make([]uuid.UUID, len(accounts))
	byID := map[uuid.UUID]*models.SocialAccount{}
	for i := range accounts {
		ids[i] = accounts[i].ID
		byID[accounts[i].ID] = &accounts[i]
	}
	sticky, err := s.Repo.StickyAccount(ctx, req.OrganizationID, req.CampaignID, req.ContactID, req.Step.Channel, ids)
	if err != nil {
		return nil, "", err
	}
	if sticky != nil {
		a := byID[*sticky]
		if !sendable(a) {
			return nil, "waiting for " + a.DisplayName + ", the " + label + " account this contact is with, to be connected again", nil
		}
		return a, "", nil
	}
	var live []*models.SocialAccount
	for i := range accounts {
		if sendable(&accounts[i]) {
			live = append(live, &accounts[i])
		}
	}
	if len(live) == 0 {
		return nil, "waiting for one of the campaign's " + label + " accounts to be connected again", nil
	}
	open, err := s.Repo.OpenActionsToday(ctx, req.OrganizationID, ids, s.now().Truncate(24*time.Hour))
	if err != nil {
		return nil, "", err
	}
	left := func(a *models.SocialAccount) int {
		limit, used := 0, 0
		var snap graphkit.SocialAccount
		if json.Unmarshal(a.Snapshot, &snap) == nil {
			for _, u := range snap.Usage {
				if u.Action == req.Step.Action {
					limit, used = u.DailyLimit, u.Today
				}
			}
		}
		return limit - used - open[a.ID][req.Step.Action]
	}
	sort.SliceStable(live, func(i, j int) bool { return left(live[i]) > left(live[j]) })
	return live[0], "", nil
}

func sendable(a *models.SocialAccount) bool {
	return a != nil && a.RemovedAt == nil && !a.UnpaidPaused && a.Status == graphkit.SocialActive
}

// cutRunes shortens text to max runes, at a word boundary when one is close; a max of 0 leaves it whole.
func cutRunes(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
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
	return models.SocialActionQueued
}

// ---- GraphKit signals -----------------------------------------------------

func (s *Service) publishAction(ctx context.Context, orgID uuid.UUID, row *models.SocialActionRow) {
	ev := pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialActionUpdated}, Channel: channelOr(row.Channel),
		Status: row.Status, ContactID: row.ContactID.String(), ActionType: row.Type}
	if row.AccountID != nil {
		ev.AccountID = row.AccountID.String()
	}
	if row.CampaignID != nil {
		ev.CampaignID = row.CampaignID.String()
	}
	s.publish(ctx, orgID, ev)
}

// applyAction records social.action.updated for an action a campaign step queued.
func (s *Service) applyAction(ctx context.Context, orgID uuid.UUID, a *graphkit.Action) error {
	if a == nil || a.ID == "" {
		return nil
	}
	u := repository.SocialActionUpdate{GraphKitID: a.ID, Status: actionStatus(a.Status), SkipReason: a.SkipReason, CompletedAt: a.CompletedAt}
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
	var change *repository.SocialStateChange
	pending, notPending := true, false
	switch row.Status {
	case models.SocialActionSucceeded:
		switch row.Type {
		case graphkit.ActionInvite:
			change = &repository.SocialStateChange{InviteSentAt: &at, InvitePending: &pending}
		case graphkit.ActionMessage, graphkit.ActionInmail, graphkit.ActionVoiceNote:
			change = &repository.SocialStateChange{MessageAt: &at}
		case graphkit.ActionWithdrawInvite:
			change = &repository.SocialStateChange{InvitePending: &notPending}
		}
		if change != nil {
			change.Bind = models.SocialBindingAction(row.Type)
		}
	case models.SocialActionSkipped:
		switch deref(a.SkipReason) {
		case "already_connected":
			change = &repository.SocialStateChange{ConnectedAt: &at, Bind: true}
		case "invite_pending":
			change = &repository.SocialStateChange{InvitePending: &pending, Bind: true}
		}
	}
	if change != nil && row.AccountID != nil {
		if err := s.Repo.NoteContactState(ctx, orgID, row.CampaignID, []uuid.UUID{row.ContactID}, channelOr(row.Channel), *row.AccountID, *change); err != nil {
			return err
		}
		if row.CampaignID != nil {
			s.wake(ctx, orgID, []string{row.CampaignID.String()})
		}
	}
	if row.CampaignID != nil && (row.Status == models.SocialActionSkipped || row.Status == models.SocialActionFailed ||
		row.Status == models.SocialActionExpired) {
		s.logStep(ctx, row, a.Recipient.FullName)
	}
	s.publishAction(ctx, orgID, row)
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
		_, err := s.api().CancelAction(ctx, ext(r.OrganizationID), r.GraphKitID, "social:cancel:"+r.ID.String())
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeStateConflict {
			// It ran or is running: record what GraphKit says rather than ask again every pass.
			if act, gerr := s.api().GetAction(ctx, ext(r.OrganizationID), r.GraphKitID); gerr == nil {
				if aerr := s.applyAction(ctx, r.OrganizationID, act); aerr != nil {
					log.Warn().Err(aerr).Str("action_id", r.GraphKitID).Msg("social: applying an action that could not be cancelled failed")
				}
			}
			continue
		}
		if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeNotFound {
			err = nil
		}
		if err != nil {
			log.Warn().Err(err).Str("action_id", r.GraphKitID).Msg("social: cancelling a queued step failed; retried on the next pass")
			continue
		}
		if err := s.Repo.FinishAction(ctx, r.OrganizationID, r.ID, models.SocialActionCancelled, strp(r.Reason), nil); err != nil {
			return err
		}
		row := &models.SocialActionRow{ID: r.ID, OrganizationID: r.OrganizationID, CampaignID: r.CampaignID, ContactID: r.ContactID,
			Channel: r.Channel, Type: r.Type, Status: models.SocialActionCancelled, SkipReason: strp(r.Reason)}
		s.logStep(ctx, row, "")
		s.publishAction(ctx, r.OrganizationID, row)
	}
	return nil
}

func (s *Service) logStep(ctx context.Context, row *models.SocialActionRow, name string) {
	if s.CampaignLogs == nil || row.CampaignID == nil {
		return
	}
	who := strings.TrimSpace(name)
	if who == "" {
		who = "a contact"
	}
	verb, event := "was skipped", "social_skipped"
	switch row.Status {
	case models.SocialActionFailed:
		verb, event = "failed", "social_failed"
	case models.SocialActionExpired:
		verb, event = "expired before it ran", "social_failed"
	case models.SocialActionCancelled:
		verb, event = "was cancelled before it ran", "social_cancelled"
	}
	reason := plainReason(row.SkipReason, row.ErrorCode)
	_ = s.CampaignLogs.CreateLog(ctx, &repository.CampaignLogEntry{CampaignID: *row.CampaignID, EventType: event,
		Message: fmt.Sprintf("%s to %s %s: %s", actionNoun(row.Channel, row.Type), who, verb, reason),
		Metadata: map[string]interface{}{"contact_id": row.ContactID.String(), "channel": channelOr(row.Channel), "action_type": row.Type,
			"status": row.Status, "skip_reason": row.SkipReason, "error_code": row.ErrorCode}})
}

// applyInvitationAccepted records a new connection and wakes the campaigns waiting on it.
func (s *Service) applyInvitationAccepted(ctx context.Context, orgID uuid.UUID, ia *graphkit.InvitationAccepted) error {
	acct, err := s.Repo.GetAccountByGraphKitID(ctx, orgID, ia.AccountID)
	if err != nil || acct == nil {
		return err
	}
	channel := channelOr(acct.Channel)
	contacts := s.contactsFor(ctx, orgID, channel, ia.Person.PublicID, ia.Person.ProfileURL, ia.Person.ProviderID)
	if len(contacts) == 0 {
		return nil
	}
	at := ia.ConnectedAt.UTC()
	if at.IsZero() {
		at = s.now()
	}
	if err := s.Repo.NoteContactState(ctx, orgID, nil, contacts, channel, acct.ID, repository.SocialStateChange{ConnectedAt: &at}); err != nil {
		return err
	}
	campaigns, err := s.Repo.ActiveCampaignsFor(ctx, orgID, contacts)
	if err != nil {
		return err
	}
	s.wake(ctx, orgID, campaigns)
	for _, c := range contacts {
		s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialActionUpdated}, Channel: channel,
			AccountID: acct.ID.String(), ContactID: c.String(), Status: "invitation_accepted", ActionType: graphkit.ActionInvite})
	}
	return nil
}

// contactsFor are the workspace's contacts any of these forms of a person names on a channel.
func (s *Service) contactsFor(ctx context.Context, orgID uuid.UUID, channel string, forms ...string) []uuid.UUID {
	for _, raw := range forms {
		_, norm, ok := models.NormalizeSocialHandle(channel, raw)
		if !ok {
			continue
		}
		if ids, err := s.Repo.ContactIDsByHandle(ctx, orgID, channel, norm); err == nil && len(ids) > 0 {
			return ids
		}
	}
	return nil
}

func (s *Service) wake(ctx context.Context, orgID uuid.UUID, campaignIDs []string) {
	if s.Campaigns != nil && len(campaignIDs) > 0 {
		s.Campaigns.WakeCampaigns(context.WithoutCancel(ctx), orgID, campaignIDs)
	}
}

// noteMessage folds a stored message into the contact's social state, and runs a new inbound one through the reply path.
func (s *Service) noteMessage(ctx context.Context, conv *models.SocialConversation, acct *models.SocialAccount, m *graphkit.SocialMessage) error {
	at := s.now()
	if m.SentAt != nil {
		at = m.SentAt.UTC()
	}
	channel := channelOr(conv.Channel)
	inbound := m.Direction == graphkit.DirectionInbound
	if conv.ContactID != nil {
		change := repository.SocialStateChange{MessageAt: &at}
		if inbound {
			change.RepliedAt = &at
		}
		if err := s.Repo.NoteContactState(ctx, conv.OrganizationID, nil, []uuid.UUID{*conv.ContactID}, channel, conv.AccountID, change); err != nil {
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
	in := models.SocialReply{OrganizationID: conv.OrganizationID, ConversationID: conv.ID, AccountID: conv.AccountID, Channel: channel,
		ContactID: conv.ContactID, Sender: firstNonEmpty(participant.FullName, participant.PublicID, "a "+models.SocialChannelLabel(channel)+" contact"),
		Body: m.Body}
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
		if xerr := s.Replies.ProcessSocialReply(context.WithoutCancel(ctx), in); xerr != nil {
			log.Warn().Err(xerr).Str("conversation_id", conv.ID.String()).Msg("social: the reply path failed for an inbound message")
		}
	}
	if conv.ContactID != nil {
		// The reply stops the contact's queued steps on every channel now; the reconcile pass catches a cancel that fails here.
		if err := s.stopActions(context.WithoutCancel(ctx), conv.ContactID); err != nil {
			log.Warn().Err(err).Str("conversation_id", conv.ID.String()).Msg("social: stopping a replying contact's queued steps failed")
		}
	}
	return nil
}

// ---- Open Profile ---------------------------------------------------------

// openProfileBatch bounds how many profiles one pass reads.
const openProfileBatch = 25

// checkOpenProfiles reads the Open Profile flag for campaign contacts a branch waits on, from one of the campaign's
// LinkedIn accounts. A contact who cannot be read is recorded as not open, so the branch never waits forever.
func (s *Service) checkOpenProfiles(ctx context.Context) error {
	checks, err := s.Repo.OpenProfileChecks(ctx, openProfileBatch)
	if err != nil {
		return err
	}
	woken := map[uuid.UUID][]string{}
	for _, c := range checks {
		open := false
		if c.Recipient != "" {
			accounts, err := s.channelAccounts(ctx, c.OrganizationID, c.CampaignID, models.SocialLinkedIn)
			if err != nil {
				return err
			}
			var acct *models.SocialAccount
			for i := range accounts {
				if sendable(&accounts[i]) {
					acct = &accounts[i]
					break
				}
			}
			if acct == nil && len(accounts) > 0 {
				// Every account is disconnected for now: ask again on a later pass.
				continue
			}
			if acct != nil {
				p, err := s.api().GetProfile(ctx, ext(c.OrganizationID), models.SocialRecipient(models.SocialLinkedIn, c.Recipient), acct.GraphKitID)
				if ge, ok := graphkit.AsError(err); err != nil && (!ok || ge.Retryable() || ge.Status >= 500) {
					log.Debug().Err(err).Str("campaign_id", c.CampaignID.String()).Msg("social: reading an Open Profile failed; tried again later")
					continue
				}
				open = err == nil && p != nil && p.Person.OpenProfile
			}
		}
		if err := s.Repo.SetOpenProfile(ctx, c.OrganizationID, c.CampaignID, c.ContactID, open); err != nil {
			return err
		}
		woken[c.OrganizationID] = append(woken[c.OrganizationID], c.CampaignID.String())
	}
	for orgID, ids := range woken {
		s.wake(ctx, orgID, ids)
	}
	return nil
}

// ---- campaign senders, activity, stats ------------------------------------

// CampaignAccountIDs are the social accounts a campaign sends its social steps from, across channels.
func (s *Service) CampaignAccountIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, error) {
	return s.Repo.CampaignAccountIDs(ctx, orgID, campaignID)
}

// SetCampaignAccounts replaces them.
func (s *Service) SetCampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) > maxLimit {
		return nil, invalid("Pick at most 100 social accounts.")
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
		return nil, errx.NewWithIdentifier(errx.NotFound, "not_found", "The campaign, or one of the social accounts, was not found.")
	}
	return s.Repo.CampaignAccountIDs(ctx, orgID, campaignID)
}

// ContactActivity is GET /v1/social/contacts/:contact_id/activity.
type ContactActivity struct {
	Actions       []models.SocialActionRow `json:"actions"`
	Conversations []*Conversation          `json:"conversations"`
}

// ContactActivity is a contact's social steps and conversations on every channel, newest first.
func (s *Service) ContactActivity(ctx context.Context, orgID, contactID uuid.UUID) (*ContactActivity, error) {
	actions, err := s.Repo.ContactActions(ctx, orgID, contactID, maxLimit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListConversations(ctx, orgID, repository.SocialConversationFilter{ContactID: &contactID, Limit: maxLimit})
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

// LeadsWithoutHandle is how many of a campaign's leads have no handle on a channel its social steps use.
func (s *Service) LeadsWithoutHandle(ctx context.Context, orgID, campaignID uuid.UUID) (int, error) {
	return s.Repo.LeadsWithoutHandle(ctx, orgID, campaignID)
}

// CampaignStats is the social block of each campaign's analytics; a campaign with no social step reads zero.
func (s *Service) CampaignStats(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID) (map[uuid.UUID]*models.SocialCampaignStats, error) {
	return s.Repo.CampaignStats(ctx, orgID, campaignIDs)
}

// UnreadConversations counts the workspace's social conversations with unread messages.
func (s *Service) UnreadConversations(ctx context.Context, orgID uuid.UUID) (int, error) {
	return s.Repo.UnreadConversations(ctx, orgID)
}
