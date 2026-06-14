package advanced

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// maxInstantChain bounds how many action nodes a single instant event can run in
// one walk, so a malformed loop in the flow graph (a chain that routes back into
// itself) can never spin. The flow editor links chains linearly, so a real
// automation is far shorter than this.
const maxInstantChain = 32

// FireInstantActions is the exported entrypoint for instant engagement triggers
// (the tracking consumer calls it with "open" / "click" after recording the
// signal). It guards the eventKind and forwards to the unexported walker. Reply
// triggers go through ProcessIncomingReply, which calls fireInstantActions
// directly with "reply". Best-effort and non-blocking, like the rest of the path.
func (s *service) FireInstantActions(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, eventKind string) {
	switch eventKind {
	case "reply", "open", "click":
	default:
		return // unknown event kind: nothing instant to fire
	}
	s.fireInstantActions(ctx, campaignID, contactID, sequenceID, eventKind)
}

// fireInstantActions runs the matched INSTANT branch's action chain for a single
// contact the MOMENT a signal lands for them (a reply is classified, an open is
// tracked, or a click is tracked), instead of waiting for the contact's next
// scheduled step boundary. eventKind selects which "it happened" fields can fire:
// "reply" (the reply_* intent fields), "open" ("opened"), or "click"
// ("clicked"). It is the instant, contact-targeted half of the branch system: the
// scheduler still handles negative branches (not_opened/not_clicked), "wait N
// days" windows, and the email steps; this only short-circuits the positive,
// instant-capable branches so "on click, do X then Y" happens immediately.
//
// Everything here is best-effort and swallows errors (logging only): the callers
// run in hot paths (the consumer's inbox path for replies, the tracking consumer
// for opens/clicks), so a CRM hiccup must never block ingest. Exactly-once PER
// (step, eventKind) is enforced by ClaimInstantFire before any side effect runs,
// so a redelivered event of the same kind cannot double-fire that kind's chain,
// while open/click/reply on the same step each fire their own chain once.
func (s *service) fireInstantActions(ctx context.Context, campaignID, contactID, currentStepID uuid.UUID, eventKind string) {
	// Load the campaign's steps with routing fields (kind/action/conditions).
	steps, err := s.campaignRepo.GetSequencesRoutingByCampaignID(ctx, campaignID)
	if err != nil {
		log.Warn().Err(err).Str("campaign_id", campaignID.String()).Str("event", eventKind).Msg("instant actions: failed to load campaign steps")
		return
	}
	if len(steps) == 0 {
		return
	}
	byID := make(map[uuid.UUID]*models.Sequence, len(steps))
	for i := range steps {
		byID[steps[i].ID] = &steps[i]
	}

	current, ok := byID[currentStepID]
	if !ok {
		return
	}

	// Decode the current step's branching tree and find the matched instant branch.
	var bc models.BranchConditions
	if len(current.Conditions) > 0 {
		if uerr := json.Unmarshal(current.Conditions, &bc); uerr != nil {
			log.Warn().Err(uerr).Str("campaign_id", campaignID.String()).Str("step_id", currentStepID.String()).Str("event", eventKind).Msg("instant actions: bad conditions json")
			return
		}
	}
	// Load the contact's CURRENT progress for this step so any engagement/window
	// conditions ANDed alongside the trigger field evaluate against real stored
	// timestamps (e.g. "clicked AND opened"). Falls back to a freshly-stamped row
	// for the trigger signal itself when no progress row is loadable yet.
	prog := s.instantProgress(ctx, campaignID, contactID, currentStepID, eventKind)

	// REUSE the scheduler's evaluator via the exported MatchInstantBranchTarget:
	// first instant branch for this eventKind in declared order whose conditions
	// all hold wins.
	matched, target, instant := repository.MatchInstantBranchTarget(&bc, prog, eventKind)
	if !matched {
		return // no instant branch on this step matches this signal
	}
	if !instant {
		return // branch opted out of instant: the scheduler routes it at the next step boundary
	}
	if target == nil {
		return // matched a STOP branch: nothing to execute instantly
	}

	// IDEMPOTENCY: claim the one-time fire for THIS event kind right BEFORE any
	// side effect. If another event of the same kind already fired this step's
	// chain, stop here.
	claimed, cerr := s.campaignProgressRepo.ClaimInstantFire(ctx, campaignID, contactID, currentStepID, eventKind)
	if cerr != nil {
		log.Warn().Err(cerr).Str("campaign_id", campaignID.String()).Str("contact_id", contactID.String()).Str("event", eventKind).Msg("instant actions: claim failed")
		return
	}
	if !claimed {
		return // already fired this kind for this step (or no progress row): no-op
	}

	// Load the contact once for templating / activity records.
	contact, xerr := s.contactRepo.GetByID(ctx, contactID)
	if xerr != nil || contact == nil {
		log.Warn().Str("campaign_id", campaignID.String()).Str("contact_id", contactID.String()).Str("event", eventKind).Msg("instant actions: contact load failed")
		return
	}
	campaign, cmErr := s.campaignRepo.GetByID(ctx, campaignID)
	if cmErr != nil || campaign == nil {
		log.Warn().Str("campaign_id", campaignID.String()).Str("event", eventKind).Msg("instant actions: campaign load failed")
		return
	}

	// Walk the linear ACTION chain from the matched branch's target. Stop at a
	// non-action node (an email step resumes via the normal scheduler), an "end",
	// or a "wait" (a wait means "not instant" — hand back to the scheduler).
	stepID := *target
	for hops := 0; hops < maxInstantChain; hops++ {
		node, live := byID[stepID]
		if !live {
			return // deleted / dangling target ends the instant chain
		}
		if node.Kind != "action" {
			return // email step (or unknown kind): resumes at the step boundary
		}
		var cfg models.ActionConfig
		if len(node.Action) > 0 {
			if uerr := json.Unmarshal(node.Action, &cfg); uerr != nil {
				log.Warn().Err(uerr).Str("campaign_id", campaignID.String()).Str("step_id", node.ID.String()).Str("event", eventKind).Msg("instant actions: bad action json")
				return
			}
		}
		// A "wait" node means "not instant" — stop here and let the normal
		// scheduler resume the contact after the wait. "end" terminates the chain.
		if cfg.Type == "wait" {
			return
		}
		if cfg.Type == "end" || cfg.Type == "" {
			return
		}

		s.executeInstantActionNode(ctx, campaign, contact, &cfg, eventKind)

		// Stamp this action node as "sent" for the contact. The scheduler's
		// FindNextRoutedPair loop-guard (sentIDs) skips steps with sent_at set, so
		// this is what stops the scheduler from re-running the very same chain when
		// it later routes the contact through this branch at the next step boundary.
		// Without it the chain would double-fire (deals/tasks/webhooks) whenever the
		// scheduler later routes the same opened/clicked/replied branch. Mirrors the
		// scheduler's own action-node bookkeeping (tasks.campaign_task).
		if rerr := s.campaignProgressRepo.RecordEmailSent(ctx, campaignID, contactID, node.ID); rerr != nil {
			log.Warn().Err(rerr).Str("campaign_id", campaignID.String()).Str("step_id", node.ID.String()).Str("event", eventKind).Msg("instant actions: failed to stamp action node sent")
		}

		// Advance to the next node in the chain by following this action node's
		// own unconditional onward branch (the editor links action chains with a
		// single catch-all connection). Anything reply-conditional or engagement-
		// conditional past here is left to the scheduler.
		next, ok := nextChainTarget(node)
		if !ok {
			return
		}
		stepID = next
	}
	log.Warn().Str("campaign_id", campaignID.String()).Str("contact_id", contactID.String()).Str("event", eventKind).Msg("instant actions: chain exceeded max hops; stopping")
}

// instantProgress builds the CampaignContactProgress the instant matcher reads.
// It loads the contact's stored progress row for this step (which already
// reflects the just-applied signal: RecordEmailOpened / RecordEmailClicked /
// RecordReplyClassification all run before this) so composite conditions like
// "clicked AND opened" evaluate against real timestamps. If no row is loadable
// yet (a rare materialization race), it falls back to a minimal row that stamps
// only the trigger signal for this eventKind, so the trigger field itself still
// matches. The reply class is always taken from the loaded row.
func (s *service) instantProgress(ctx context.Context, campaignID, contactID, stepID uuid.UUID, eventKind string) *repository.CampaignContactProgress {
	prog := &repository.CampaignContactProgress{
		CampaignID: campaignID,
		ContactID:  contactID,
		SequenceID: stepID,
	}
	if rows, err := s.campaignProgressRepo.GetContactProgress(ctx, campaignID, contactID); err == nil {
		for i := range rows {
			if rows[i].SequenceID == stepID {
				r := rows[i]
				prog = &r
				break
			}
		}
	}
	// Guarantee the trigger signal is present even if the just-applied write has
	// not propagated into the read above. Never clears a signal already loaded.
	now := time.Now().UTC()
	switch eventKind {
	case "open":
		if prog.OpenedAt == nil {
			prog.OpenedAt = &now
		}
	case "click":
		if prog.ClickedAt == nil {
			prog.ClickedAt = &now
		}
	}
	return prog
}

// nextChainTarget returns the single unconditional onward target of an action
// node (the catch-all branch the editor draws between chained action steps), so
// the instant walker can follow "action -> action -> ..." without re-evaluating
// reply/engagement predicates. A node with no unconditional branch ends the
// instant chain (any conditional routing past it is the scheduler's job).
func nextChainTarget(node *models.Sequence) (uuid.UUID, bool) {
	if len(node.Conditions) == 0 {
		return uuid.Nil, false
	}
	var bc models.BranchConditions
	if err := json.Unmarshal(node.Conditions, &bc); err != nil {
		return uuid.Nil, false
	}
	for i := range bc.Branches {
		b := &bc.Branches[i]
		if len(b.Conditions) == 0 && b.TargetSequenceID != nil {
			return *b.TargetSequenceID, true
		}
	}
	return uuid.Nil, false
}

// executeInstantActionNode runs one action node's control-plane side effect for a
// contact, NOW, in response to an instant signal (reply / open / click).
// eventKind is surfaced as the "trigger" on emitted events / logs. It mirrors
// tasks.executeActionNode but stays inside the advanced service (advanced cannot
// import tasks — tasks imports advanced), reusing the same repos and the
// CreateContactDeal / MoveContactDealStage methods. Best-effort: each action logs
// and continues so one bad node never aborts the rest of the chain or blocks the
// caller's hot path.
func (s *service) executeInstantActionNode(ctx context.Context, campaign *models.Campaign, contact *models.Contact, cfg *models.ActionConfig, eventKind string) {
	switch cfg.Type {
	case "add_tag":
		if cfg.CategoryID == nil {
			return
		}
		if _, xerr := s.contactRepo.Update(ctx, campaign.UserID, contact.ID.String(), &models.UpdateContact{
			AddCategories: []string{cfg.CategoryID.String()},
		}); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "remove_tag":
		if cfg.CategoryID == nil {
			return
		}
		if _, xerr := s.contactRepo.Update(ctx, campaign.UserID, contact.ID.String(), &models.UpdateContact{
			RemoveCategories: []string{cfg.CategoryID.String()},
		}); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "label_email":
		// Label the conversation the contact just replied on. The most recent
		// thread for the contact in the campaign owner's unibox is that reply.
		if len(cfg.LabelIDs) == 0 {
			return
		}
		owner, perr := uuid.Parse(campaign.UserID)
		if perr != nil {
			return
		}
		if _, xerr := s.LabelLatestThreadForContact(ctx, owner, contact.Email, cfg.LabelIDs); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "unsubscribe":
		if xerr := s.Unsubscribe(ctx, campaign.ID, contact.ID); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "create_task":
		if campaign.OrganizationID == nil {
			return
		}
		owner, perr := uuid.Parse(campaign.UserID)
		if perr != nil {
			return
		}
		title := strings.TrimSpace(cfg.TaskTitle)
		if title == "" {
			title = "Follow up: " + contactDisplayName(contact)
		}
		assignee := cfg.TaskAssignedTo
		if assignee == nil {
			assignee = &owner
		}
		cid := contact.ID
		data := &models.CreateCRMTask{
			ContactID:  &cid,
			Title:      title,
			Type:       cfg.TaskType,
			Priority:   cfg.TaskPriority,
			AssignedTo: assignee,
		}
		if cfg.TaskDueOffsetDays != nil {
			due := time.Now().UTC().AddDate(0, 0, *cfg.TaskDueOffsetDays)
			data.DueDate = &due
		}
		if _, xerr := s.CreateContactTask(ctx, *campaign.OrganizationID, owner, data); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "create_deal":
		if campaign.OrganizationID == nil {
			return
		}
		if cfg.DealPipelineID == nil || cfg.DealStageID == nil {
			return // misconfigured node: skip rather than abort the chain
		}
		owner, perr := uuid.Parse(campaign.UserID)
		if perr != nil {
			return
		}
		name := renderContactTemplate(strings.TrimSpace(cfg.DealName), contact)
		if name == "" {
			name = "Deal: " + contactDisplayName(contact)
		}
		currency := strings.TrimSpace(cfg.DealCurrency)
		if currency == "" {
			currency = "USD"
		}
		cid := contact.ID
		cmpID := campaign.ID
		data := &models.CreateDeal{
			PipelineID: *cfg.DealPipelineID,
			StageID:    *cfg.DealStageID,
			ContactID:  &cid,
			Name:       name,
			Value:      cfg.DealValue,
			Currency:   currency,
			CampaignID: &cmpID,
			AssignedTo: &owner,
		}
		if _, xerr := s.CreateContactDeal(ctx, *campaign.OrganizationID, owner, data); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "move_deal_stage":
		if campaign.OrganizationID == nil {
			return
		}
		if cfg.DealPipelineID == nil || cfg.DealStageID == nil {
			return
		}
		if _, xerr := s.MoveContactDealStage(ctx, *campaign.OrganizationID, contact.ID, *cfg.DealPipelineID, *cfg.DealStageID); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "run_automation":
		// Mirror tasks.executeActionNode's run_automation case so an automation
		// placed directly on a reply/open/click branch fires NOW. Without this the
		// walker would stamp the node sent (below) with the automation never run,
		// and the scheduler's sentIDs guard would then skip it forever.
		if s.automationRunner == nil || campaign.OrganizationID == nil || cfg.AutomationID == nil {
			return
		}
		data := map[string]any{
			"campaign_id":   campaign.ID.String(),
			"campaign_name": campaign.Name,
			"contact_id":    contact.ID.String(),
			"contact_email": contact.Email,
			"first_name":    contact.FirstName,
			"last_name":     contact.LastName,
			"company":       contact.Company,
			"phone":         contact.Phone,
			"trigger":       eventKind,
			// Stable per-(campaign,contact,trigger) key so a duplicate instant
			// delivery dedupes downstream (same contract as the scheduler path).
			"idempotency_key": fmt.Sprintf("campaign:%s:%s:%s", campaign.ID, contact.ID, eventKind),
		}
		for _, kv := range cfg.AutomationValues {
			key := strings.TrimSpace(kv.Key)
			if key == "" {
				continue
			}
			data[key] = renderContactTemplate(kv.Value, contact)
		}
		if xerr := s.automationRunner.RunAutomationByID(ctx, *campaign.OrganizationID, *cfg.AutomationID, data); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	case "fire_event":
		if campaign.OrganizationID == nil {
			return
		}
		s.FireCampaignEvent(ctx, *campaign.OrganizationID, campaign.ID.String(), cfg.EventName, cfg.EventFields, contact)
	case "http_request":
		if campaign.OrganizationID == nil {
			return
		}
		if xerr := s.RunCampaignHTTPRequest(ctx, *campaign.OrganizationID, cfg, contact); xerr != nil {
			s.logActionErr(campaign, contact, cfg.Type, eventKind, xerr)
		}
	default:
		// "wait" / "end" are handled by the chain walker (they stop the walk);
		// unknown types are ignored.
	}
}

func (s *service) logActionErr(campaign *models.Campaign, contact *models.Contact, action, eventKind string, err error) {
	log.Warn().
		Str("campaign_id", campaign.ID.String()).
		Str("contact_id", contact.ID.String()).
		Str("action", action).
		Str("trigger", eventKind).
		Msg(fmt.Sprintf("instant action failed: %v", err))
}

func contactDisplayName(contact *models.Contact) string {
	name := strings.TrimSpace(contact.FirstName + " " + contact.LastName)
	if name == "" {
		return contact.Email
	}
	return name
}

// renderContactTemplate substitutes {{.FirstName}}-style placeholders in a deal
// name. It mirrors the naive substitution path of tasks.RenderTemplate (the same
// {{.Field}} contract used on the canvas) without importing tasks, which would
// cycle (tasks imports advanced). Standard fields plus custom fields are
// supported; unknown tokens are left untouched.
func renderContactTemplate(tmpl string, contact *models.Contact) string {
	if tmpl == "" || contact == nil {
		return tmpl
	}
	out := tmpl
	out = strings.ReplaceAll(out, "{{.FirstName}}", contact.FirstName)
	out = strings.ReplaceAll(out, "{{.LastName}}", contact.LastName)
	out = strings.ReplaceAll(out, "{{.Email}}", contact.Email)
	out = strings.ReplaceAll(out, "{{.Company}}", contact.Company)
	out = strings.ReplaceAll(out, "{{.Phone}}", contact.Phone)
	for k, v := range contact.CustomFields {
		out = strings.ReplaceAll(out, "{{."+k+"}}", v)
	}
	return out
}
