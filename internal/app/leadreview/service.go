// Package leadreview is the Review tab: a campaign's readiness report, each
// lead's emails as they will be sent, per-lead copies, and approving leads
// before their first email when the campaign reviews leads.
package leadreview

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks"
)

const (
	// MaxBatch bounds the leads one approve, reopen or write call names.
	MaxBatch = 500
	// WritePassSize is how many leads one write-ahead pass handles.
	WritePassSize = 10
	writeParallel = 4

	defaultListLimit = 50
	maxListLimit     = 200
)

// Campaigns is the slice of the campaign service this package uses.
type Campaigns interface {
	Get(ctx context.Context, orgID, id string) (*models.Campaign, *errx.Error)
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

// Contacts loads one organization's contacts.
type Contacts interface {
	Load(ctx context.Context, orgID, contactID uuid.UUID) (*models.Contact, *errx.Error)
}

// RepoContacts loads contacts through the organization-scoped contact repository.
type RepoContacts struct {
	Repo repository.ContactRepository
}

func (r RepoContacts) Load(ctx context.Context, orgID, contactID uuid.UUID) (*models.Contact, *errx.Error) {
	found, xerr := r.Repo.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{contactID})
	if xerr != nil {
		return nil, xerr
	}
	if len(found) == 0 {
		return nil, errx.New(errx.NotFound, "contact not found")
	}
	c := found[0]
	if c.CustomFields == nil {
		c.CustomFields = map[string]string{}
	}
	return &c, nil
}

// Undeliverable counts leads verification rejected.
type Undeliverable interface {
	CountUndeliverableLeads(ctx context.Context, campaignID uuid.UUID) (int, error)
}

// Steps lists a campaign's steps in position order.
type Steps interface {
	GetCampaignSequences(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, error)
}

// Service is the Review tab's backend.
type Service struct {
	Campaigns     Campaigns
	Contacts      Contacts
	Repo          repository.CampaignReviewRepository
	Emails        tasks.LeadEmailsService
	Steps         Steps
	Undeliverable Undeliverable
}

func (s *Service) campaign(ctx context.Context, orgID, campaignID uuid.UUID) (*models.Campaign, *errx.Error) {
	c, xerr := s.Campaigns.Get(ctx, orgID.String(), campaignID.String())
	if xerr != nil {
		return nil, xerr
	}
	// Get is scoped to orgID but leaves OrganizationID unset; AI writing and billing need it.
	c.OrganizationID = &orgID
	return c, nil
}

func emailSteps(all []models.Sequence) []models.Sequence {
	out := make([]models.Sequence, 0, len(all))
	for _, st := range all {
		if st.Kind == "" || st.Kind == "email" {
			out = append(out, st)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Position < out[b].Position })
	return out
}

// gaps is every built-in field the campaign's emails print with nothing to fall back on.
func gaps(steps []models.Sequence) []string {
	seen := map[string]bool{}
	for _, st := range steps {
		for _, part := range []string{st.Subject, st.BodyHTML, st.BodyPlain} {
			g, ok := tmplvars.BlankGaps(part)
			if !ok {
				g = tmplvars.References(part)
			}
			for k := range g {
				seen[k] = true
			}
		}
	}
	var out []string
	for _, f := range models.MissingFieldNames {
		if seen[f] {
			out = append(out, f)
		}
	}
	return out
}

func (s *Service) steps(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, *errx.Error) {
	all, err := s.Steps.GetCampaignSequences(ctx, campaignID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return emailSteps(all), nil
}

// Summary is the readiness report: how leads split by review state, the merge
// fields some of them would leave blank, what sending will skip, and how much
// of the first email's AI copy is written ahead.
func (s *Service) Summary(ctx context.Context, orgID, campaignID uuid.UUID) (*models.CampaignReviewSummary, *errx.Error) {
	campaign, xerr := s.campaign(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	steps, xerr := s.steps(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	counts, err := s.Repo.Counts(ctx, campaignID)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := &models.CampaignReviewSummary{CampaignID: campaignID, ReviewLeads: campaign.ReviewLeads, Counts: *counts, Missing: []models.CampaignMissingField{}}

	missing, err := s.Repo.MissingCounts(ctx, campaignID, gaps(steps))
	if err != nil {
		return nil, errx.InternalError()
	}
	for f, n := range missing {
		if n > 0 {
			out.Missing = append(out.Missing, models.CampaignMissingField{Field: f, Leads: n})
		}
	}
	sort.Slice(out.Missing, func(a, b int) bool {
		if out.Missing[a].Leads != out.Missing[b].Leads {
			return out.Missing[a].Leads > out.Missing[b].Leads
		}
		return out.Missing[a].Field < out.Missing[b].Field
	})

	for _, st := range steps {
		if err := tmplvars.CheckCopy(
			tmplvars.Part{Name: "subject", Text: st.Subject},
			tmplvars.Part{Name: "body", Text: st.BodyHTML, HTML: true},
			tmplvars.Part{Name: "plain text", Text: st.BodyPlain},
		); err != nil {
			out.TemplateErrors = append(out.TemplateErrors, models.CampaignStepIssue{StepID: st.ID, Position: st.Position, Message: err.Error()})
		}
	}

	if s.Undeliverable != nil {
		if out.Undeliverable, err = s.Undeliverable.CountUndeliverableLeads(ctx, campaignID); err != nil {
			return nil, errx.InternalError()
		}
	}
	if out.Unsubscribed, err = s.Repo.UnsubscribedCount(ctx, campaignID); err != nil {
		return nil, errx.InternalError()
	}

	if len(steps) > 0 {
		first := steps[0]
		blocks := s.Emails.AIBlockIDs(first.BodyHTML)
		if len(blocks) > 0 {
			ids := blockIDs(blocks)
			written, pending, err := s.Repo.AIProgress(ctx, campaignID, first.ID, ids)
			if err != nil {
				return nil, errx.InternalError()
			}
			var per models.Micro
			for _, b := range blocks {
				if b.Research {
					per += credits.EstimateResearchRun
				} else {
					per += credits.EstimateWritingAssistant
				}
			}
			id := first.ID
			out.AI = models.CampaignAIReadiness{
				StepID: &id, Blocks: len(blocks), Written: written, Pending: pending,
				EstimatedCostUSD: (per * models.Micro(pending)).USD(),
				Auto:             campaign.Status == "active" && s.Emails.AIAvailable(),
			}
		}
	}
	return out, nil
}

func blockIDs(blocks []tasks.AIBlockRef) []string {
	ids := make([]string, len(blocks))
	for i, b := range blocks {
		ids[i] = b.ID
	}
	return ids
}

// EncodeCursor and DecodeCursor make the review list's keyset position opaque.
func EncodeCursor(c models.CampaignReviewCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.Email + "\x1f" + c.ContactID.String()))
}

// ErrInvalidCursor is a cursor this package did not issue.
var ErrInvalidCursor = errors.New("invalid cursor")

func DecodeCursor(raw string) (*models.CampaignReviewCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	email, id, ok := strings.Cut(string(b), "\x1f")
	if !ok {
		return nil, ErrInvalidCursor
	}
	contactID, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &models.CampaignReviewCursor{Email: email, ContactID: contactID}, nil
}

// ListLeads pages the campaign's leads with their review state.
func (s *Service) ListLeads(ctx context.Context, orgID, campaignID uuid.UUID, f models.CampaignReviewLeadsFilter) (*models.CampaignReviewLeadsResult, *errx.Error) {
	if _, xerr := s.campaign(ctx, orgID, campaignID); xerr != nil {
		return nil, xerr
	}
	if !models.ReviewStatusFilters[f.Status] {
		return nil, errx.New(errx.BadRequest, "status must be one of all, waiting, ready, held or started")
	}
	if f.Missing != "" && !contains(models.MissingFieldNames, f.Missing) {
		return nil, errx.New(errx.BadRequest, "missing must be one of "+strings.Join(models.MissingFieldNames, ", "))
	}
	if f.Limit == 0 {
		f.Limit = defaultListLimit
	}
	if f.Limit < 1 || f.Limit > maxListLimit {
		return nil, errx.New(errx.BadRequest, "limit must be between 1 and 200")
	}
	steps, xerr := s.steps(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	f.Gaps = gaps(steps)
	rows, more, err := s.Repo.ListLeads(ctx, campaignID, f)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := &models.CampaignReviewLeadsResult{Data: rows, Pagination: models.CPagination{HasMore: more}}
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		next := EncodeCursor(models.CampaignReviewCursor{Email: last.Email, ContactID: last.ContactID})
		out.Pagination.NextCursor = &next
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// SetReview turns review mode on or off. Off approves every waiting lead, so
// the caller gates it on the send permission.
func (s *Service) SetReview(ctx context.Context, orgID, campaignID uuid.UUID, enabled bool) (*models.CampaignReviewChange, *errx.Error) {
	if _, xerr := s.campaign(ctx, orgID, campaignID); xerr != nil {
		return nil, xerr
	}
	held, released, err := s.Repo.SetReviewLeads(ctx, campaignID, enabled)
	if err != nil {
		log.Error().Err(err).Str("campaign_id", campaignID.String()).Msg("set review mode failed")
		return nil, errx.InternalError()
	}
	if released > 0 {
		s.Campaigns.WakeCampaigns(ctx, orgID, []string{campaignID.String()})
	}
	return &models.CampaignReviewChange{CampaignID: campaignID, ReviewLeads: enabled, Held: held, Released: released}, nil
}

// Approve releases waiting leads, the listed ones or all of them.
func (s *Service) Approve(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID, all bool) (*models.CampaignReviewChange, *errx.Error) {
	campaign, xerr := s.campaign(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := checkBatch(ids, all); xerr != nil {
		return nil, xerr
	}
	n, err := s.Repo.ApproveLeads(ctx, campaignID, ids, all)
	if err != nil {
		return nil, errx.InternalError()
	}
	if n > 0 {
		s.Campaigns.WakeCampaigns(ctx, orgID, []string{campaignID.String()})
	}
	return &models.CampaignReviewChange{CampaignID: campaignID, ReviewLeads: campaign.ReviewLeads, Released: n}, nil
}

// Reopen sends leads not yet emailed back to waiting for review.
func (s *Service) Reopen(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID) (*models.CampaignReviewChange, *errx.Error) {
	campaign, xerr := s.campaign(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := checkBatch(ids, false); xerr != nil {
		return nil, xerr
	}
	if !campaign.ReviewLeads {
		return nil, errx.New(errx.BadRequest, "review mode is off; turn it on to hold leads for review")
	}
	n, err := s.Repo.ReopenLeads(ctx, campaignID, ids)
	if err != nil {
		return nil, errx.InternalError()
	}
	return &models.CampaignReviewChange{CampaignID: campaignID, ReviewLeads: campaign.ReviewLeads, Held: n}, nil
}

func checkBatch(ids []uuid.UUID, all bool) *errx.Error {
	if all && len(ids) > 0 {
		return errx.New(errx.BadRequest, "send either contact_ids or all, not both")
	}
	if !all && len(ids) == 0 {
		return errx.New(errx.BadRequest, "contact_ids is required")
	}
	if len(ids) > MaxBatch {
		return errx.New(errx.BadRequest, "at most 500 contact_ids per request")
	}
	return nil
}

func (s *Service) lead(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.Campaign, *models.Contact, *models.CampaignReviewLead, *errx.Error) {
	campaign, xerr := s.campaign(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, nil, nil, xerr
	}
	lead, err := s.Repo.GetLead(ctx, campaignID, contactID)
	if errors.Is(err, repository.ErrLeadNotInCampaign) {
		return nil, nil, nil, errx.New(errx.NotFound, "contact is not a lead of this campaign")
	}
	if err != nil {
		return nil, nil, nil, errx.InternalError()
	}
	contact, xerr := s.Contacts.Load(ctx, orgID, contactID)
	if xerr != nil {
		return nil, nil, nil, xerr
	}
	return campaign, contact, lead, nil
}

// LeadEmails renders every email step for one lead, the way it will be sent.
func (s *Service) LeadEmails(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.LeadEmails, *errx.Error) {
	campaign, contact, lead, xerr := s.lead(ctx, orgID, campaignID, contactID)
	if xerr != nil {
		return nil, xerr
	}
	steps, from, xerr := s.Emails.LeadEmails(ctx, orgID, campaign, contact)
	if xerr != nil {
		return nil, xerr
	}
	seen := map[string]bool{}
	for _, st := range steps {
		for _, m := range st.Missing {
			if !seen[m] {
				seen[m] = true
				lead.Missing = append(lead.Missing, m)
			}
		}
	}
	return &models.LeadEmails{CampaignID: campaignID, Lead: *lead, From: from, Steps: steps}, nil
}

func (s *Service) emailStep(ctx context.Context, campaignID, stepID uuid.UUID) (*models.Sequence, *errx.Error) {
	steps, xerr := s.steps(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	for i := range steps {
		if steps[i].ID == stepID {
			return &steps[i], nil
		}
	}
	return nil, errx.New(errx.NotFound, "email step not found in this campaign")
}

// errStepSent refuses a change to an email this lead already received.
func errStepSent() *errx.Error {
	return errx.NewWithIdentifier(errx.Conflict, "lead_step_sent", "This lead already received this email, so its copy can no longer change.")
}

// Personalize stores this lead's own copy of one email step. It is checked
// like a step's copy and replaces the step, and any A/B variant, for this lead.
func (s *Service) Personalize(ctx context.Context, orgID, campaignID, contactID, stepID uuid.UUID, editor *uuid.UUID, in models.LeadStepOverrideInput) (*models.LeadStepOverride, *errx.Error) {
	if _, _, _, xerr := s.lead(ctx, orgID, campaignID, contactID); xerr != nil {
		return nil, xerr
	}
	if _, xerr := s.emailStep(ctx, campaignID, stepID); xerr != nil {
		return nil, xerr
	}
	if strings.TrimSpace(in.BodyHTML) == "" && strings.TrimSpace(in.BodyPlain) == "" {
		return nil, errx.New(errx.BadRequest, "the email needs a body")
	}
	if len([]rune(in.Subject)) > 998 || len(in.BodyHTML) > 524288 || len(in.BodyPlain) > 262144 {
		return nil, errx.New(errx.BadRequest, "the email is too long")
	}
	if err := tmplvars.CheckCopy(
		tmplvars.Part{Name: "subject", Text: in.Subject},
		tmplvars.Part{Name: "body", Text: in.BodyHTML, HTML: true},
		tmplvars.Part{Name: "plain text", Text: in.BodyPlain},
	); err != nil {
		return nil, errx.InvalidTemplate(err)
	}
	o, err := s.Repo.UpsertOverride(ctx, models.LeadStepOverride{
		CampaignID: campaignID, ContactID: contactID, StepID: stepID,
		Subject: in.Subject, BodyHTML: in.BodyHTML, BodyPlain: in.BodyPlain, EditedBy: editor,
	})
	if errors.Is(err, repository.ErrLeadStepSent) {
		return nil, errStepSent()
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	return o, nil
}

// ResetPersonalization drops this lead's own copy, so the step's copy goes out again.
func (s *Service) ResetPersonalization(ctx context.Context, orgID, campaignID, contactID, stepID uuid.UUID) (bool, *errx.Error) {
	if _, _, _, xerr := s.lead(ctx, orgID, campaignID, contactID); xerr != nil {
		return false, xerr
	}
	if _, xerr := s.emailStep(ctx, campaignID, stepID); xerr != nil {
		return false, xerr
	}
	removed, err := s.Repo.DeleteOverride(ctx, campaignID, contactID, stepID)
	if errors.Is(err, repository.ErrLeadStepSent) {
		return false, errStepSent()
	}
	if err != nil {
		return false, errx.InternalError()
	}
	return removed, nil
}

// WriteAI writes one lead's AI copy for one step now, charged as it would be at
// send time; rewrite replaces copy already written, and is charged again.
func (s *Service) WriteAI(ctx context.Context, orgID, campaignID, contactID, stepID uuid.UUID, rewrite bool) *errx.Error {
	campaign, contact, _, xerr := s.lead(ctx, orgID, campaignID, contactID)
	if xerr != nil {
		return xerr
	}
	return s.Emails.WriteLeadAI(ctx, orgID, campaign, contact, stepID, rewrite)
}

// WriteAIBatch writes the first email's AI copy (or the named step's) for one
// pass of leads still missing it: the listed leads, else every waiting lead
// when waitingOnly, else every lead not yet emailed. A caller repeats it until
// Remaining is zero, so a closed tab or a low balance loses nothing written.
func (s *Service) WriteAIBatch(ctx context.Context, orgID, campaignID uuid.UUID, stepID *uuid.UUID, ids []uuid.UUID, waitingOnly bool, after *models.CampaignReviewCursor) (*models.LeadAIBatchResult, *errx.Error) {
	campaign, xerr := s.campaign(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if len(ids) > MaxBatch {
		return nil, errx.New(errx.BadRequest, "at most 500 contact_ids per request")
	}
	steps, xerr := s.steps(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if len(steps) == 0 {
		return &models.LeadAIBatchResult{}, nil
	}
	step := &steps[0]
	if stepID != nil {
		if step, xerr = s.emailStep(ctx, campaignID, *stepID); xerr != nil {
			return nil, xerr
		}
	}
	blocks := blockIDs(s.Emails.AIBlockIDs(step.BodyHTML))
	if len(blocks) == 0 {
		return &models.LeadAIBatchResult{}, nil
	}
	pass, total, err := s.Repo.PendingAILeads(ctx, campaignID, step.ID, blocks, ids, waitingOnly, after, WritePassSize)
	if err != nil {
		return nil, errx.InternalError()
	}

	out := &models.LeadAIBatchResult{}
	var mu sync.Mutex
	stop := ""
	sem := make(chan struct{}, writeParallel)
	var wg sync.WaitGroup
	for _, lead := range pass {
		id := lead.ContactID
		mu.Lock()
		halted := stop != ""
		mu.Unlock()
		if halted {
			break
		}
		sem <- struct{}{}
		// A worker that finished while this waited may have stopped the pass.
		mu.Lock()
		halted = stop != ""
		mu.Unlock()
		if halted {
			<-sem
			break
		}
		wg.Add(1)
		go func(contactID uuid.UUID) {
			defer wg.Done()
			defer func() { <-sem }()
			contact, xerr := s.Contacts.Load(ctx, orgID, contactID)
			if xerr == nil {
				xerr = s.Emails.WriteLeadAI(ctx, orgID, campaign, contact, step.ID, false)
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case xerr == nil:
				out.Written++
			case xerr.ResponseCode() == "insufficient_credits" || xerr.ResponseCode() == "usage_cap_exceeded":
				stop = xerr.ResponseCode()
			default:
				out.Failed++
			}
		}(id)
	}
	wg.Wait()
	out.Stopped = stop
	// A stopped pass resumes where it began, so nothing it did not reach is
	// skipped; a finished one moves past every lead it tried, failed ones included.
	if stop == "" && len(pass) > 0 {
		out.Next = EncodeCursor(pass[len(pass)-1])
		out.Remaining = total - len(pass)
	} else {
		if after != nil {
			out.Next = EncodeCursor(*after)
		}
		out.Remaining = total - out.Written
	}
	if out.Remaining < 0 {
		out.Remaining = 0
	}
	return out, nil
}
