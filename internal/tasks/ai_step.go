package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// Campaign "ai" sequence step. One LLM call per contact passing through the
// step: the model follows the step's instruction over the contact's data and
// returns a JSON object carrying the chosen label (stored on the progress row
// for ai_label branch routing), the configured contact custom fields (merged
// onto the contact, usable as {{.field}} in later emails), and/or the ids of
// the pre-configured actions to trigger for this contact. The model only
// decides WHICH configured actions run — parameters, targets, and the allowed
// set are all user-authored, and every decision lands in the campaign log.
// Same lifecycle as the automation AI nodes: gate -> consume(Idempotency-Key)
// -> call -> refund-on-failure, deterministic sampling, bounded output.
const (
	seqAIMaxTokens = 512
	seqAITimeout   = 20 * time.Second
	seqAIValueCap  = 2000
)

// execSequenceAIStep runs an "ai" node for one contact. Errors surface to the
// action-node caller, which logs the step as skipped in the campaign log; the
// contact keeps routing (an unlabeled contact falls to the branch catch-all).
func (s *tasksService) execSequenceAIStep(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequenceID uuid.UUID, cfg *models.ActionConfig) error {
	instruction := strings.TrimSpace(RenderTemplate(cfg.AIInstruction, *contact))
	if instruction == "" {
		return nil // unconfigured draft node: harmless no-op like other action types
	}
	labels := aiNonEmpty(cfg.AILabels)
	fields := aiNonEmpty(cfg.AIOutputFields)
	acts := usableAIStepActions(cfg.AIActions)
	if len(labels) == 0 && len(fields) == 0 && len(acts) == 0 {
		return errors.New("this AI step has no labels, output fields, or actions")
	}
	if s.aiProvider == nil || s.aiCredits == nil {
		return errors.New("AI steps are not available on this deployment")
	}
	if campaign.OrganizationID == nil {
		return errors.New("AI steps need an organization-owned campaign")
	}

	// The key is stable per (campaign, contact, step), so an at-least-once task
	// redelivery never double-charges. A free/local model runs un-metered.
	model := s.aiProvider.ModelForTier(false)
	idemKey := fmt.Sprintf("seq_ai:%s:%s:%s", campaign.ID, contact.ID, sequenceID)
	if !s.aiProvider.IsLocal() {
		if _, cerr := s.aiCredits.Consume(ctx, *campaign.OrganizationID, credits.CostCampaignAIStep, "campaign_ai", model, 0, idemKey); cerr != nil {
			switch {
			case errors.Is(cerr, credits.ErrInsufficientCredits):
				return fmt.Errorf("out of AI credits: this step needs %d credit", credits.CostCampaignAIStep)
			case errors.Is(cerr, credits.ErrCapExceeded):
				return errors.New("AI usage cap reached; try again later")
			default:
				return cerr
			}
		}
	}

	// Ground the model in "what happened so far": the contact's campaign
	// history and their newest inbound email. Both are per-step opt-outs.
	history := ""
	if !cfg.AINoEngagement {
		history = s.campaignHistoryContext(ctx, campaign, contact)
	}
	reply := ""
	if !cfg.AINoReplies {
		reply = s.latestReplyContext(ctx, campaign, contact)
	}

	cctx, cancel := context.WithTimeout(ctx, seqAITimeout)
	defer cancel()

	system, prompt := buildSequenceAIPrompt(campaign, contact, instruction, labels, fields, acts, history, reply)
	res, gerr := s.aiProvider.Complete(cctx, generation.CompletionRequest{
		System:      system,
		Prompt:      prompt,
		Model:       model,
		MaxTokens:   seqAIMaxTokens,
		Temperature: generation.Deterministic(),
	})
	if gerr != nil || res == nil {
		// The org paid for a step the provider couldn't complete: refund it (a
		// local model was never charged).
		if !s.aiProvider.IsLocal() {
			_, _ = s.aiCredits.Grant(ctx, *campaign.OrganizationID, credits.CostCampaignAIStep, "campaign_ai_refund")
		}
		if gerr != nil {
			return fmt.Errorf("AI step failed: %w", gerr)
		}
		return errors.New("AI step returned no output")
	}

	return s.applySequenceAIOutput(ctx, campaign, contact, sequenceID, labels, fields, acts, res.Text)
}

// usableAIStepActions drops malformed entries (blank id, model-callable-only
// types). Type safety is enforced on write; the "ai" guard here is the runtime
// recursion backstop.
func usableAIStepActions(in []models.AIStepAction) []models.AIStepAction {
	out := make([]models.AIStepAction, 0, len(in))
	for _, a := range in {
		if strings.TrimSpace(a.ID) == "" {
			continue
		}
		switch a.Action.Type {
		case "ai", "wait", "end", "":
			continue
		}
		out = append(out, a)
	}
	return out
}

// campaignHistoryContext renders what already happened for this contact in
// this campaign as bounded "step: signals" lines, so the model can decide
// based on the journey so far (steps sent, opens/clicks/replies, reply intent,
// prior AI labels). Best-effort: any load failure just yields "".
func (s *tasksService) campaignHistoryContext(ctx context.Context, campaign *models.Campaign, contact *models.Contact) string {
	rows, err := s.campaignProgressRepo.GetContactProgress(ctx, campaign.ID, contact.ID)
	if err != nil || len(rows) == 0 {
		return ""
	}
	names := map[uuid.UUID]string{}
	if seqs, serr := s.campaignRepo.GetSequencesByCampaignID(ctx, campaign.ID); serr == nil {
		for i := range seqs {
			names[seqs[i].ID] = seqs[i].Name
		}
	}
	var b strings.Builder
	for i := range rows {
		if i >= 20 {
			break
		}
		r := &rows[i]
		name := strings.TrimSpace(names[r.SequenceID])
		if name == "" {
			name = "step " + r.SequenceID.String()[:8]
		}
		b.WriteString("- ")
		b.WriteString(name)
		if r.SentAt != nil {
			b.WriteString(" (ran " + r.SentAt.Format("2006-01-02") + ")")
		}
		var sig []string
		if r.OpenedAt != nil {
			sig = append(sig, "opened")
		}
		if r.ClickedAt != nil {
			sig = append(sig, "clicked")
		}
		if r.RepliedAt != nil {
			sig = append(sig, "replied")
		}
		if r.ReplyClass != "" {
			sig = append(sig, "reply intent: "+r.ReplyClass)
		}
		if r.AILabel != "" {
			sig = append(sig, "AI label: "+r.AILabel)
		}
		if len(sig) > 0 {
			b.WriteString(": " + strings.Join(sig, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// latestReplyContext renders the newest email received from the contact
// (subject + snippet), so the model can react to what they actually wrote.
func (s *tasksService) latestReplyContext(ctx context.Context, campaign *models.Campaign, contact *models.Contact) string {
	if s.advanced == nil {
		return ""
	}
	owner, err := uuid.Parse(campaign.UserID)
	if err != nil {
		return ""
	}
	subject, snippet, lerr := s.advanced.LatestInboundFromContact(ctx, owner, contact.Email)
	if lerr != nil || (subject == "" && snippet == "") {
		return ""
	}
	return "Subject: " + aiTruncate(subject, 200) + "\n" + aiTruncate(snippet, 600)
}

// buildSequenceAIPrompt frames the model call: a strict JSON reply whose keys
// are the label slot (when the step classifies), each output field, and the
// action array (when the step may trigger actions).
func buildSequenceAIPrompt(campaign *models.Campaign, contact *models.Contact, instruction string, labels, fields []string, acts []models.AIStepAction, history, reply string) (system, prompt string) {
	keys := make([]string, 0, len(fields)+2)
	if len(labels) > 0 {
		keys = append(keys, `"label" (EXACTLY one of: `+strings.Join(labels, ", ")+`)`)
	}
	for _, f := range fields {
		keys = append(keys, `"`+f+`" (a string; "" when not determinable)`)
	}
	hasChoices := false
	for i := range acts {
		if len(acts[i].Choices) > 0 {
			hasChoices = true
			break
		}
	}
	if len(acts) > 0 {
		if hasChoices {
			keys = append(keys, `"actions" (an array; each entry is an action id string, or {"id": "...", "choices": ["..."]} for actions that list choices; [] for none)`)
		} else {
			keys = append(keys, `"actions" (an array with the ids of the possible actions that should run for this contact; [] for none)`)
		}
	}
	system = "You are an AI step in an email outreach sequence. Follow the instruction using the contact data and history. Reply with ONLY a JSON object with these keys: " + strings.Join(keys, ", ") + ". No prose, no code fences."

	var b strings.Builder
	b.WriteString("Instruction: ")
	b.WriteString(instruction)
	b.WriteString("\n\nCampaign: ")
	b.WriteString(campaign.Name)
	if len(acts) > 0 {
		b.WriteString("\n\nPossible actions (run only the ones that fit; none is a valid choice):\n")
		for _, a := range acts {
			b.WriteString("- id: ")
			b.WriteString(a.ID)
			b.WriteString(" | action: ")
			b.WriteString(a.Action.Type)
			if w := strings.TrimSpace(a.When); w != "" {
				b.WriteString(" | run when: ")
				b.WriteString(aiTruncate(w, 200))
			}
			if len(a.Choices) > 0 {
				names := make([]string, 0, len(a.Choices))
				for _, c := range a.Choices {
					names = append(names, strings.TrimSpace(c.Name))
				}
				b.WriteString(" | choices (pick any that apply")
				if a.MaxChoices > 0 {
					b.WriteString(fmt.Sprintf(", at most %d", a.MaxChoices))
				}
				b.WriteString("): ")
				b.WriteString(strings.Join(names, ", "))
			}
			b.WriteString("\n")
		}
	}
	if history != "" {
		b.WriteString("\nCampaign history for this contact:\n")
		b.WriteString(history)
	}
	if reply != "" {
		b.WriteString("\nNewest email received from the contact:\n")
		b.WriteString(reply)
		b.WriteString("\n")
	}
	b.WriteString("\nContact data:\n")
	b.WriteString(contactAIContext(contact))
	return system, b.String()
}

// contactAIContext renders the contact's fields as bounded "key: value" lines.
func contactAIContext(contact *models.Contact) string {
	var b strings.Builder
	add := func(k, v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(aiTruncate(v, 400))
		b.WriteString("\n")
	}
	add("first_name", contact.FirstName)
	add("last_name", contact.LastName)
	add("email", contact.Email)
	add("company", contact.Company)
	add("phone", contact.Phone)
	n := 0
	for k, v := range contact.CustomFields {
		if n >= 40 {
			break
		}
		add(k, v)
		n++
	}
	if b.Len() == 0 {
		return "(no fields)"
	}
	return b.String()
}

// applySequenceAIOutput parses the model's JSON and writes the results, in a
// deliberate order:
//  1. output fields onto the contact (merged into custom_fields — existing
//     keys the model didn't fill survive), ALSO merged into the in-memory
//     contact so the chosen actions' templated params ({{.field}}) can use
//     values the model just wrote (AI-authored deal names, task titles, event
//     payload values — declare a field, reference it in the action template);
//  2. the chosen actions via the normal action-node executor;
//  3. the routing label onto the progress row.
func (s *tasksService) applySequenceAIOutput(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequenceID uuid.UUID, labels, fields []string, acts []models.AIStepAction, text string) error {
	vals := parseSequenceAIJSON(text)

	var fieldErr error
	if len(fields) > 0 {
		cf := map[string]string{}
		for _, k := range fields {
			if v := strings.TrimSpace(aiValueString(vals[k])); v != "" {
				cf[k] = aiTruncate(v, seqAIValueCap)
			}
		}
		if len(cf) > 0 {
			if contact.CustomFields == nil {
				contact.CustomFields = map[string]string{}
			}
			for k, v := range cf {
				contact.CustomFields[k] = v
			}
			if _, xerr := s.contactRepo.Update(ctx, campaign.UserID, contact.ID.String(), *campaign.OrganizationID, &models.UpdateContact{
				CustomFields: &cf,
			}); xerr != nil {
				fieldErr = xerr
			}
		}
	}

	// Actions run even when the field write failed (the in-memory merge still
	// feeds their templates); each outcome is logged individually and an
	// action failure never aborts the rest.
	if len(acts) > 0 {
		s.runAIChosenActions(ctx, campaign, contact, sequenceID, acts, vals["actions"])
	}

	if len(labels) > 0 {
		label := matchSequenceAILabel(aiValueString(vals["label"]), labels)
		if label == "" {
			// Unmatched answer: leave the contact unlabeled (branch catch-all)
			// rather than storing free text a branch can never match.
			return fmt.Errorf("AI did not pick one of the configured labels: %s", aiTruncate(aiValueString(vals["label"]), 80))
		}
		if err := s.campaignProgressRepo.RecordAILabel(ctx, campaign.ID, contact.ID, sequenceID, label); err != nil {
			return err
		}
	}
	return fieldErr
}

// runAIChosenActions executes the configured actions the model picked. Only
// ids from the configured set run, each at most once, in the order the user
// configured them (not the model's order, so "add tag then run automation"
// stays deterministic). For actions with a Choices set the model's picks are
// resolved against the allowed names (case-insensitive) and capped at
// MaxChoices before executing. Every run and every failure is written to the
// campaign log so AI decisions stay auditable; a failing action is logged and
// skipped, never fatal for the step.
func (s *tasksService) runAIChosenActions(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequenceID uuid.UUID, acts []models.AIStepAction, raw any) {
	chosen := parseAIActionPicks(raw)
	for i := range acts {
		act := &acts[i]
		picks, ok := chosen[act.ID]
		if !ok {
			continue
		}
		detail := ""
		var aerr error
		if len(act.Choices) > 0 {
			ids, names := resolveAIChoices(act, picks)
			if len(ids) == 0 {
				// The model picked the action but none of its allowed choices:
				// nothing to do, recorded as a skip so the decision stays visible.
				aerr = errors.New("AI picked none of the allowed choices")
			} else {
				detail = " (" + strings.Join(names, ", ") + ")"
				aerr = s.execAIChoiceAction(ctx, campaign, contact, sequenceID, act, ids)
			}
		} else {
			aerr = s.executeActionNode(ctx, campaign, contact, sequenceID, &act.Action)
		}
		if s.campaignLogRepo != nil {
			evt, msg := "action", fmt.Sprintf("AI step ran '%s'%s for %s", act.Action.Type, detail, contact.Email)
			if aerr != nil {
				evt, msg = "action_skipped", fmt.Sprintf("AI step chose '%s' for %s but it did not run: %v", act.Action.Type, contact.Email, aerr)
			}
			_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  evt,
				Message:    msg,
			})
		}
	}
}

// parseAIActionPicks reads the model's "actions" array. Each entry is either a
// bare action id ("a1") or an object with per-action choices
// ({"id": "a1", "choices": ["VIP", "Warm"]}). Returns id -> chosen names.
func parseAIActionPicks(raw any) map[string][]string {
	out := map[string][]string{}
	arr, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, v := range arr {
		switch t := v.(type) {
		case string:
			if id := strings.TrimSpace(t); id != "" {
				if _, dup := out[id]; !dup {
					out[id] = nil
				}
			}
		case map[string]any:
			id := strings.TrimSpace(aiValueString(t["id"]))
			if id == "" {
				continue
			}
			var names []string
			if ch, cok := t["choices"].([]any); cok {
				for _, c := range ch {
					if n := strings.TrimSpace(aiValueString(c)); n != "" {
						names = append(names, n)
					}
				}
			}
			out[id] = names
		}
	}
	return out
}

// resolveAIChoices maps the model's chosen names onto the action's allowed
// choice set (case-insensitive), deduped, capped at MaxChoices (0 = no cap).
func resolveAIChoices(act *models.AIStepAction, picks []string) ([]uuid.UUID, []string) {
	max := act.MaxChoices
	if max <= 0 || max > len(act.Choices) {
		max = len(act.Choices)
	}
	var ids []uuid.UUID
	var names []string
	used := map[uuid.UUID]bool{}
	for _, p := range picks {
		for i := range act.Choices {
			c := &act.Choices[i]
			if used[c.CategoryID] || !strings.EqualFold(strings.TrimSpace(c.Name), p) {
				continue
			}
			used[c.CategoryID] = true
			ids = append(ids, c.CategoryID)
			names = append(names, strings.TrimSpace(c.Name))
			break
		}
		if len(ids) >= max {
			break
		}
	}
	return ids, names
}

// execAIChoiceAction runs a tag/label action over the AI-chosen targets by
// synthesizing per-target configs for the normal executor (one call per tag;
// one call with the full set for thread labels). Returns the first error.
func (s *tasksService) execAIChoiceAction(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequenceID uuid.UUID, act *models.AIStepAction, ids []uuid.UUID) error {
	switch act.Action.Type {
	case "add_tag", "remove_tag":
		var firstErr error
		for i := range ids {
			cfg := act.Action
			id := ids[i]
			cfg.CategoryID = &id
			if err := s.executeActionNode(ctx, campaign, contact, sequenceID, &cfg); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	case "label_email":
		cfg := act.Action
		cfg.LabelIDs = ids
		return s.executeActionNode(ctx, campaign, contact, sequenceID, &cfg)
	default:
		// Validation only allows choices on the types above; anything else is
		// a stale config — run it as configured rather than dropping it.
		return s.executeActionNode(ctx, campaign, contact, sequenceID, &act.Action)
	}
}

// parseSequenceAIJSON parses the model's JSON object, tolerating a ```json
// fence. A parse failure yields an empty map so every key falls back to "".
func parseSequenceAIJSON(text string) map[string]any {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j >= i {
			text = text[i : j+1]
		}
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(text), &out)
	return out
}

// matchSequenceAILabel maps the model's answer onto the configured label set
// (case-insensitive exact, then prefix, then substring). Unlike the automation
// matcher it returns "" on a miss: only configured labels are stored, because
// the ai_label branches can only ever match those.
func matchSequenceAILabel(text string, labels []string) string {
	got := strings.ToLower(strings.Trim(strings.TrimSpace(text), ".\"'` \n\t"))
	if got == "" {
		return ""
	}
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), got) {
			return l
		}
	}
	for _, l := range labels {
		if strings.HasPrefix(got, strings.ToLower(strings.TrimSpace(l))) {
			return l
		}
	}
	for _, l := range labels {
		if strings.Contains(got, strings.ToLower(strings.TrimSpace(l))) {
			return l
		}
	}
	return ""
}

func aiNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func aiValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func aiTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
