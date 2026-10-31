package nodescope

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// fakeAssignments is the assignment a worker would read from email_accounts, with the grace already applied.
type fakeAssignments struct {
	roles     map[uuid.UUID]models.NodeRole
	mailboxes map[uuid.UUID][]repository.NodeMailbox
	keys      map[uuid.UUID]string
	campaigns map[uuid.UUID]uuid.UUID
	loads     int
}

func (f *fakeAssignments) NodeRole(_ context.Context, id uuid.UUID) (models.NodeRole, bool, error) {
	r, ok := f.roles[id]
	return r, ok, nil
}

func (f *fakeAssignments) WorkerMailboxes(_ context.Context, w uuid.UUID, _ time.Duration) ([]repository.NodeMailbox, error) {
	f.loads++
	return f.mailboxes[w], nil
}

func (f *fakeAssignments) WorkerHoldsMailbox(_ context.Context, w, user, box uuid.UUID, _ time.Duration) (bool, error) {
	for _, m := range f.mailboxes[w] {
		if m.ID == box && (user == uuid.Nil || m.UserID == user) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeAssignments) WorkerHoldsOrganization(_ context.Context, w, org uuid.UUID, _ time.Duration) (bool, error) {
	for _, m := range f.mailboxes[w] {
		if m.OrganizationID != nil && *m.OrganizationID == org {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeAssignments) OrganizationDataKeys(_ context.Context, orgs []uuid.UUID) ([]string, error) {
	var out []string
	for _, o := range orgs {
		if k, ok := f.keys[o]; ok {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeAssignments) DataKeyOrganization(_ context.Context, sealed string) (uuid.UUID, bool, error) {
	for o, k := range f.keys {
		if k == sealed {
			return o, true, nil
		}
	}
	return uuid.Nil, false, nil
}

func (f *fakeAssignments) CampaignOrganization(_ context.Context, c uuid.UUID) (uuid.UUID, bool, error) {
	o, ok := f.campaigns[c]
	return o, ok, nil
}

// fakeGrants answers EXISTS for the keys Grant would have written.
type fakeGrants struct {
	redis.Cmdable
	keys map[string]bool
}

func (g *fakeGrants) Exists(_ context.Context, keys ...string) *redis.IntCmd {
	n := int64(0)
	for _, k := range keys {
		if g.keys[k] {
			n++
		}
	}
	return redis.NewIntResult(n, nil)
}

func TestScopeBindsAWorkerToItsAssignment(t *testing.T) {
	ctx := context.Background()
	worker, otherWorker, consumer, unknown := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	user, org, otherOrg, grantedOrg := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mailbox, movedAway, otherMailbox, campaign, otherCampaign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &fakeAssignments{
		roles: map[uuid.UUID]models.NodeRole{worker: models.NodeRoleWorker, otherWorker: models.NodeRoleWorker, consumer: models.NodeRoleConsumer},
		mailboxes: map[uuid.UUID][]repository.NodeMailbox{
			// movedAway left this worker within the release grace, which the query applies.
			worker:      {{ID: mailbox, UserID: user, OrganizationID: &org}, {ID: movedAway, UserID: user, OrganizationID: &org}},
			otherWorker: {{ID: otherMailbox, UserID: user, OrganizationID: &otherOrg}},
		},
		keys:      map[uuid.UUID]string{org: "sealed-org", otherOrg: "sealed-other", grantedOrg: "sealed-granted"},
		campaigns: map[uuid.UUID]uuid.UUID{campaign: org, otherCampaign: otherOrg},
	}
	grants := &fakeGrants{keys: map[string]bool{grantKey(worker, grantedOrg): true}}
	s := New(repo, grants)

	type probe struct {
		name string
		run  func(node uuid.UUID) (bool, error)
	}
	probes := []probe{
		{"own worker id", func(n uuid.UUID) (bool, error) { return s.Worker(ctx, n, worker) }},
		{"assigned mailbox", func(n uuid.UUID) (bool, error) { return s.Mailbox(ctx, n, user, mailbox) }},
		{"mailbox moved off within the grace", func(n uuid.UUID) (bool, error) { return s.Mailbox(ctx, n, user, movedAway) }},
		{"assigned organization", func(n uuid.UUID) (bool, error) { return s.Organization(ctx, n, org) }},
		{"granted organization", func(n uuid.UUID) (bool, error) { return s.Organization(ctx, n, grantedOrg) }},
		{"assigned data key", func(n uuid.UUID) (bool, error) { return s.DataKey(ctx, n, "sealed-org") }},
		{"granted data key", func(n uuid.UUID) (bool, error) { return s.DataKey(ctx, n, "sealed-granted") }},
		{"assigned mailbox body", func(n uuid.UUID) (bool, error) {
			return s.BlobKey(ctx, n, "users/"+user.String()+"/emails/"+mailbox.String()+"/m.emsg")
		}},
		{"assigned campaign attachment", func(n uuid.UUID) (bool, error) {
			return s.BlobKey(ctx, n, "attachments/"+campaign.String()+"/a.pdf")
		}},
		{"transport body", func(n uuid.UUID) (bool, error) {
			return s.BlobKey(ctx, n, "emails/2026/10/31/"+uuid.NewString()+".emsg")
		}},
	}
	refusals := []probe{
		{"another worker's id", func(n uuid.UUID) (bool, error) { return s.Worker(ctx, n, otherWorker) }},
		{"another worker's mailbox", func(n uuid.UUID) (bool, error) { return s.Mailbox(ctx, n, user, otherMailbox) }},
		{"own mailbox under another user", func(n uuid.UUID) (bool, error) { return s.Mailbox(ctx, n, uuid.New(), mailbox) }},
		{"unassigned organization", func(n uuid.UUID) (bool, error) { return s.Organization(ctx, n, otherOrg) }},
		{"unassigned data key", func(n uuid.UUID) (bool, error) { return s.DataKey(ctx, n, "sealed-other") }},
		{"unknown data key", func(n uuid.UUID) (bool, error) { return s.DataKey(ctx, n, "sealed-nobody") }},
		{"another worker's mailbox body", func(n uuid.UUID) (bool, error) {
			return s.BlobKey(ctx, n, "users/"+user.String()+"/emails/"+otherMailbox.String()+"/m.emsg")
		}},
		{"unassigned campaign attachment", func(n uuid.UUID) (bool, error) {
			return s.BlobKey(ctx, n, "attachments/"+otherCampaign.String()+"/a.pdf")
		}},
		{"malformed mailbox key", func(n uuid.UUID) (bool, error) { return s.BlobKey(ctx, n, "users/x/notes") }},
	}

	for _, p := range probes {
		if ok, err := p.run(worker); err != nil || !ok {
			t.Fatalf("worker: %s refused (%v)", p.name, err)
		}
	}
	for _, p := range refusals {
		if ok, err := p.run(worker); err != nil || ok {
			t.Fatalf("worker: %s allowed (%v)", p.name, err)
		}
		if ok, err := p.run(consumer); err != nil || !ok {
			t.Fatalf("consumer: %s refused; a consumer serves every organization (%v)", p.name, err)
		}
		if ok, err := p.run(unknown); err != nil || ok {
			t.Fatalf("node with no row: %s allowed (%v)", p.name, err)
		}
	}
}

func TestScopeSeesAFreshAssignmentAtOnce(t *testing.T) {
	ctx := context.Background()
	worker, user, org, mailbox := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &fakeAssignments{
		roles:     map[uuid.UUID]models.NodeRole{worker: models.NodeRoleWorker},
		mailboxes: map[uuid.UUID][]repository.NodeMailbox{},
		keys:      map[uuid.UUID]string{org: "sealed-org"},
	}
	s := New(repo, nil)
	if ok, _ := s.Mailbox(ctx, worker, user, mailbox); ok {
		t.Fatal("an unassigned mailbox is out of scope")
	}
	// Assigned after the scope was cached: the miss asks the database directly.
	repo.mailboxes[worker] = []repository.NodeMailbox{{ID: mailbox, UserID: user, OrganizationID: &org}}
	if ok, _ := s.Mailbox(ctx, worker, user, mailbox); !ok {
		t.Fatal("a fresh assignment must be in scope without waiting for the cache")
	}
	loads := repo.loads
	if ok, _ := s.DataKey(ctx, worker, "sealed-org"); !ok || repo.loads != loads+1 {
		t.Fatalf("the hit reloads the stale scope once (loads %d -> %d)", loads, repo.loads)
	}
	for range 100 {
		if ok, _ := s.DataKey(ctx, worker, "sealed-org"); !ok {
			t.Fatal("cached data key refused")
		}
	}
	if repo.loads != loads+1 {
		t.Fatalf("hot data-key checks must be answered from the cache, loaded %d times", repo.loads-loads)
	}
}
