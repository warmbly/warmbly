package leadreview

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks"
)

type aheadRepo struct {
	repository.CampaignReviewRepository
	campaigns []repository.CampaignOrg
	mu        sync.Mutex
	// pending holds each campaign's leads still missing their AI copy, by email.
	pending map[uuid.UUID][]models.CampaignReviewCursor
}

func (r *aheadRepo) ActiveAICampaigns(_ context.Context, after uuid.UUID, limit int) ([]repository.CampaignOrg, error) {
	var out []repository.CampaignOrg
	for _, c := range r.campaigns {
		if c.CampaignID.String() > after.String() && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *aheadRepo) PendingAILeads(_ context.Context, campaignID, _ uuid.UUID, _ []string, _ []uuid.UUID, _ bool, after *models.CampaignReviewCursor, limit int) ([]models.CampaignReviewCursor, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var rest []models.CampaignReviewCursor
	for _, l := range r.pending[campaignID] {
		if after == nil || l.Email > after.Email {
			rest = append(rest, l)
		}
	}
	if len(rest) > limit {
		return rest[:limit], len(rest), nil
	}
	return rest, len(rest), nil
}

func (r *aheadRepo) written(campaignID, contactID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.pending[campaignID]
	for i, l := range list {
		if l.ContactID == contactID {
			r.pending[campaignID] = append(list[:i:i], list[i+1:]...)
			return
		}
	}
}

type aheadEmails struct {
	tasks.LeadEmailsService
	repo   *aheadRepo
	broke  map[uuid.UUID]bool // orgs out of balance
	failed map[uuid.UUID]bool // contacts whose writing fails
	mu     sync.Mutex
	calls  map[uuid.UUID]int
}

func (e *aheadEmails) AIAvailable() bool { return true }

func (e *aheadEmails) AIBlockIDs(string) []tasks.AIBlockRef {
	return []tasks.AIBlockRef{{ID: "b1"}}
}

func (e *aheadEmails) WriteLeadAI(_ context.Context, orgID uuid.UUID, c *models.Campaign, contact *models.Contact, _ uuid.UUID, _ bool) *errx.Error {
	e.mu.Lock()
	e.calls[contact.ID]++
	e.mu.Unlock()
	// The real writer refuses a campaign without its organization.
	if c.OrganizationID == nil || *c.OrganizationID != orgID {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "ai_generation_failed", "no organization")
	}
	if e.broke[orgID] {
		return errx.NewWithIdentifier(errx.PaymentRequired, "insufficient_credits", "balance")
	}
	if e.failed[contact.ID] {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "ai_generation_failed", "failed")
	}
	e.repo.written(c.ID, contact.ID)
	return nil
}

type aheadCampaigns struct{}

func (aheadCampaigns) Get(_ context.Context, orgID, id string) (*models.Campaign, *errx.Error) {
	return &models.Campaign{ID: uuid.MustParse(id), Status: "active"}, nil
}
func (aheadCampaigns) WakeCampaigns(context.Context, uuid.UUID, []string) {}

type aheadContacts struct{}

func (aheadContacts) Load(_ context.Context, _, id uuid.UUID) (*models.Contact, *errx.Error) {
	return &models.Contact{ID: id}, nil
}

type aheadSteps struct{}

func (aheadSteps) GetCampaignSequences(context.Context, uuid.UUID) ([]models.Sequence, error) {
	return []models.Sequence{{ID: uuid.New(), Kind: "email", BodyHTML: `<span data-ai-var="b1"></span>`}}, nil
}

func leads(n int) []models.CampaignReviewCursor {
	out := make([]models.CampaignReviewCursor, n)
	for i := range out {
		out[i] = models.CampaignReviewCursor{Email: fmt.Sprintf("lead%03d@example.com", i), ContactID: uuid.New()}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Email < out[b].Email })
	return out
}

// Every lead of an active campaign gets written over a few ticks, a lead that
// keeps failing neither stalls the rest nor is given up on, and a workspace out
// of balance stops after one attempt per tick.
func TestWriteAheadWritesEveryLeadAndStopsAtTheBalance(t *testing.T) {
	paying, broke := uuid.New(), uuid.New()
	big, small, unpaid := uuid.New(), uuid.New(), uuid.New()
	bigLeads, smallLeads, unpaidLeads := leads(75), leads(3), leads(20)
	repo := &aheadRepo{
		campaigns: []repository.CampaignOrg{{CampaignID: big, OrgID: paying}, {CampaignID: small, OrgID: paying}, {CampaignID: unpaid, OrgID: broke}},
		pending: map[uuid.UUID][]models.CampaignReviewCursor{
			big: append([]models.CampaignReviewCursor(nil), bigLeads...), small: smallLeads, unpaid: unpaidLeads,
		},
	}
	sort.Slice(repo.campaigns, func(a, b int) bool {
		return repo.campaigns[a].CampaignID.String() < repo.campaigns[b].CampaignID.String()
	})
	stuck := bigLeads[0].ContactID
	emails := &aheadEmails{repo: repo, broke: map[uuid.UUID]bool{broke: true}, failed: map[uuid.UUID]bool{stuck: true}, calls: map[uuid.UUID]int{}}
	s := &Service{Campaigns: aheadCampaigns{}, Contacts: aheadContacts{}, Repo: repo, Emails: emails, Steps: aheadSteps{}}

	cursors := map[uuid.UUID]*models.CampaignReviewCursor{}
	s.writeAheadTick(context.Background(), cursors)
	if got := len(repo.pending[small]); got != 0 {
		t.Fatalf("small campaign has %d leads unwritten after one tick", got)
	}
	if got := len(repo.pending[big]); got != 75-writeAheadPasses*WritePassSize+1 {
		t.Fatalf("big campaign has %d unwritten after one tick, want one tick's worth written", got)
	}
	for tick := 0; tick < 3; tick++ {
		s.writeAheadTick(context.Background(), cursors)
	}
	if got := len(repo.pending[big]); got != 1 || repo.pending[big][0].ContactID != stuck {
		t.Fatalf("big campaign pending = %v, want only the failing lead", repo.pending[big])
	}
	if emails.calls[stuck] < 2 {
		t.Fatalf("a failing lead was tried %d times; the list should wrap and retry it", emails.calls[stuck])
	}
	unpaidCalls := 0
	for _, l := range unpaidLeads {
		unpaidCalls += emails.calls[l.ContactID]
	}
	// Every tick reaches the balance check once, and stops before a second round of writers.
	if len(repo.pending[unpaid]) != 20 || unpaidCalls < 4 || unpaidCalls > 4*writeParallel {
		t.Fatalf("workspace without balance: %d pending, %d attempts over 4 ticks", len(repo.pending[unpaid]), unpaidCalls)
	}

	// A campaign that stops being active loses its cursor.
	repo.campaigns = nil
	s.writeAheadTick(context.Background(), cursors)
	if len(cursors) != 0 {
		t.Fatalf("cursors kept for inactive campaigns: %v", cursors)
	}
}
