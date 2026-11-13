package contact

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type limitByOrg map[uuid.UUID]int

func (l limitByOrg) ContactLimit(_ context.Context, orgID uuid.UUID) (int, *errx.Error) {
	return l[orgID], nil
}

type limitContacts struct {
	repository.ContactRepository
	byOrg map[uuid.UUID][]string
}

func (f limitContacts) GetContactCount(_ context.Context, orgID uuid.UUID) (int, *errx.Error) {
	return len(f.byOrg[orgID]), nil
}

func (f limitContacts) ImportLookup(_ context.Context, orgID, _ uuid.UUID, emails []string) (map[string]uuid.UUID, map[string]bool, *errx.Error) {
	existing := map[string]uuid.UUID{}
	for _, held := range f.byOrg[orgID] {
		for _, e := range emails {
			if strings.EqualFold(held, e) {
				existing[e] = uuid.New()
			}
		}
	}
	return existing, map[string]bool{}, nil
}

func addresses(n int, domain string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuid.NewString()[:8] + "@" + domain
	}
	return out
}

// The ceiling is the workspace's, over the workspace's contacts, whichever member is adding them.
func TestContactLimitFollowsTheWorkspace(t *testing.T) {
	team, trial := uuid.New(), uuid.New()
	svc := &contactService{
		limits:            limitByOrg{team: 10000, trial: 100},
		contactRepository: limitContacts{byOrg: map[uuid.UUID][]string{team: addresses(150, "team.test"), trial: addresses(90, "trial.test")}},
	}
	ctx := context.Background()

	if xerr := svc.checkContactLimit(ctx, team, 500); xerr != nil {
		t.Fatalf("paid workspace refused 500 more contacts: %v", xerr.Message)
	}
	if xerr := svc.checkContactLimit(ctx, trial, 20); xerr == nil || xerr.Identifier != "contact_limit_reached" {
		t.Fatalf("trial workspace past its 100: got %v, want contact_limit_reached", xerr)
	}
	if xerr := svc.checkContactLimit(ctx, team, 9851); xerr == nil {
		t.Fatal("paid workspace accepted contacts past its ceiling")
	}
}

// At the ceiling, re-adding an address the workspace already holds is an update, not a new contact.
func TestContactLimitCountsOnlyNewAddresses(t *testing.T) {
	org := uuid.New()
	held := addresses(100, "full.test")
	svc := &contactService{
		limits:            limitByOrg{org: 100},
		contactRepository: limitContacts{byOrg: map[uuid.UUID][]string{org: held}},
	}
	ctx := context.Background()
	user := uuid.NewString()

	fresh, xerr := svc.newAddresses(ctx, user, org, []models.AddContact{{Email: strings.ToUpper(held[0])}, {Email: "Dana <" + held[1] + ">"}, {Email: " " + held[1]}})
	if xerr != nil || fresh != 0 {
		t.Fatalf("re-adding held addresses counted %d new (err %v), want 0", fresh, xerr)
	}
	fresh, xerr = svc.newAddresses(ctx, user, org, []models.AddContact{{Email: held[0]}, {Email: "someone@else.test"}})
	if xerr != nil || fresh != 1 {
		t.Fatalf("one new address counted %d (err %v), want 1", fresh, xerr)
	}
}
