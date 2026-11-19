package partner

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (f *liveFixture) withMailboxes(t *testing.T) {
	t.Helper()
	f.svc.WireMailboxes(MailboxDeps{Repo: repository.NewPartnerMailboxRepository(f.pool)})
	t.Cleanup(func() {
		bg := context.Background()
		for _, p := range []*models.Partner{f.a, f.b} {
			_, _ = f.pool.Exec(bg, `DELETE FROM partner_mailboxes WHERE partner_id = $1`, p.ID)
			_, _ = f.pool.Exec(bg, `DELETE FROM partner_groups WHERE partner_id = $1`, p.ID)
		}
	})
}

func tokenAttach(enablement string) AttachInput {
	return AttachInput{Source: models.PartnerSourceInfrastructure, Email: "ada@getacme.test", Provider: "google", Enablement: enablement,
		Connection: AttachConnection{Method: "token"}}
}

// A mailbox is attached, enabled, listed and detached only by the partner and mode that own its workspace.
func TestLivePartnerMailboxesAreScopedToPartnerAndMode(t *testing.T) {
	f := liveSetup(t)
	f.withMailboxes(t)
	ctx := context.Background()
	aLive := f.caller(t, f.a, models.PartnerModeLive)
	aTest := f.caller(t, f.a, models.PartnerModeTest)
	bLive := f.caller(t, f.b, models.PartnerModeLive)
	if _, _, xerr := f.svc.Upsert(ctx, aLive, "ws-1", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}

	m, created, xerr := f.svc.Attach(ctx, aLive, "ws-1", "omb_1", tokenAttach("warmup_only"))
	if xerr != nil || !created || m.Status != "connecting" || m.Enablement != "warmup_only" || m.Source != "graphkit_infrastructure" ||
		m.InfrastructureMailboxID == nil || *m.InfrastructureMailboxID != "omb_1" || m.DailySendLimit != 50 {
		t.Fatalf("attach = %+v created=%v %v", m, created, xerr)
	}
	again, created, xerr := f.svc.Attach(ctx, aLive, "ws-1", "omb_1", tokenAttach("sending_warmup"))
	if xerr != nil || created || again.ID != m.ID || again.Enablement != "sending_warmup" {
		t.Fatalf("re-attach = %+v created=%v %v", again, created, xerr)
	}

	for name, c := range map[string]*Caller{"same partner, test mode": aTest, "another partner": bLive} {
		if _, _, xerr := f.svc.Attach(ctx, c, "ws-1", "omb_2", tokenAttach("off")); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s attached a mailbox: %v", name, xerr)
		}
		if _, xerr := f.svc.SetEnablement(ctx, c, "ws-1", m.ID, "off"); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s changed an enablement: %v", name, xerr)
		}
		if xerr := f.svc.Detach(ctx, c, "ws-1", "omb_1"); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s detached a mailbox: %v", name, xerr)
		}
		if _, xerr := f.svc.ListMailboxes(ctx, c, "ws-1", "", 100); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s listed the mailboxes: %v", name, xerr)
		}
	}
	// The same partner id in another of the partner's workspaces is another mailbox, not this one.
	if _, _, xerr := f.svc.Upsert(ctx, aLive, "ws-2", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := f.svc.SetEnablement(ctx, aLive, "ws-2", m.ID, "off"); xerr == nil || xerr.Code != errx.NotFound {
		t.Errorf("another workspace's id reached the mailbox: %v", xerr)
	}

	page, xerr := f.svc.ListMailboxes(ctx, aLive, "ws-1", "", 100)
	if xerr != nil || len(page.Data) != 1 || page.Data[0].ID != m.ID {
		t.Fatalf("list = %+v %v", page, xerr)
	}
	v, xerr := f.svc.Get(ctx, aLive, "ws-1")
	if xerr != nil || v.Usage.Enabled.SendingWarmup != 1 || v.Usage.Enabled.WarmupOnly != 0 {
		t.Fatalf("usage.enabled = %+v %v", v.Usage.Enabled, xerr)
	}

	// Suspended: turning a mailbox on is refused, turning it off is not.
	if _, _, xerr := f.svc.Upsert(ctx, aLive, "ws-1", orgInput("suspended")); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := f.svc.SetEnablement(ctx, aLive, "ws-1", m.ID, "warmup_only"); xerr == nil || xerr.Identifier != "organization_suspended" {
		t.Fatalf("enabling in a suspended workspace = %v", xerr)
	}
	if off, xerr := f.svc.SetEnablement(ctx, aLive, "ws-1", m.ID, "off"); xerr != nil || off.Enablement != "off" {
		t.Fatalf("turning off in a suspended workspace = %+v %v", off, xerr)
	}

	if xerr := f.svc.Detach(ctx, aLive, "ws-1", "omb_1"); xerr != nil {
		t.Fatal(xerr)
	}
	if page, _ := f.svc.ListMailboxes(ctx, aLive, "ws-1", "", 100); len(page.Data) != 0 {
		t.Fatalf("a detached mailbox is still listed: %+v", page.Data)
	}
	if xerr := f.svc.Detach(ctx, aLive, "ws-1", "omb_unknown"); xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("detaching an unknown mailbox = %v", xerr)
	}
}

// fakeDelegator records how each mailbox was connected; it holds one proven grant domain.
type fakeDelegator struct {
	proven    string
	purchased []string
	delegated map[string]bool
}

func (d *fakeDelegator) ConnectPurchased(_ context.Context, _, _ uuid.UUID, _, address, _, _ string, brokered bool) (*models.Email, *errx.Error) {
	if !brokered {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "unexpected", "the partner path recorded a grant from its own input")
	}
	d.purchased = append(d.purchased, address)
	return &models.Email{ID: uuid.New(), Email: address}, nil
}

func (d *fakeDelegator) ConnectPartnerDelegated(_ context.Context, _, _ uuid.UUID, _, address, _ string, vouched bool) (*models.Email, *errx.Error) {
	d.delegated[address] = vouched
	return &models.Email{ID: uuid.New(), Email: address}, nil
}

func (d *fakeDelegator) PartnerDelegationGrant(_ context.Context, _ uuid.UUID, _, address string) (*models.DomainGrant, *errx.Error) {
	if strings.HasSuffix(address, "@"+d.proven) {
		return &models.DomainGrant{ID: uuid.New()}, nil
	}
	return nil, nil
}

func delegationAttach(email string) AttachInput {
	return AttachInput{Source: models.PartnerSourceInfrastructure, Email: email, Provider: "google", Connection: AttachConnection{Method: "delegation"}}
}

// A delegation attach is accepted only for a domain the workspace proved or the operator allowed, and a suspended
// partner's attached mailboxes wait instead of connecting.
func TestLivePartnerDelegationNeedsProofOrAnAllowedDomain(t *testing.T) {
	f := liveSetup(t)
	del := &fakeDelegator{proven: "proven.test", delegated: map[string]bool{}}
	f.svc.WireMailboxes(MailboxDeps{Repo: repository.NewPartnerMailboxRepository(f.pool), Delegator: del})
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM partner_mailboxes WHERE partner_id = $1`, f.a.ID)
	})
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	if _, _, xerr := f.svc.Upsert(ctx, c, "ws-d", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	if _, _, xerr := f.svc.Attach(ctx, c, "ws-d", "omb_victim", delegationAttach("ceo@victim.test")); xerr == nil || xerr.Identifier != "delegation_not_allowed" {
		t.Fatalf("unproven domain attach = %v", xerr)
	}
	if _, _, xerr := f.svc.Attach(ctx, c, "ws-d", "omb_proven", delegationAttach("bob@proven.test")); xerr != nil {
		t.Fatalf("proven domain attach = %v", xerr)
	}
	domains := []string{"Infra.Test"}
	if _, xerr := f.svc.UpdatePartner(ctx, f.a.ID, UpdatePartnerInput{DelegationDomains: &domains}); xerr != nil {
		t.Fatal(xerr)
	}
	c = f.caller(t, f.a, models.PartnerModeLive)
	if !slices.Equal(c.Partner.DelegationDomains, []string{"infra.test"}) {
		t.Fatalf("delegation domains = %v", c.Partner.DelegationDomains)
	}
	if _, _, xerr := f.svc.Attach(ctx, c, "ws-d", "omb_infra", delegationAttach("amy@infra.test")); xerr != nil {
		t.Fatalf("allowed domain attach = %v", xerr)
	}
	if _, _, xerr := f.svc.Attach(ctx, c, "ws-d", "omb_tok", tokenAttach("off")); xerr != nil {
		t.Fatalf("token attach = %v", xerr)
	}
	due := func() {
		if _, err := f.pool.Exec(ctx, `UPDATE partner_mailboxes SET next_connect_at = now() WHERE partner_id = $1 AND connect_state = 'pending'`, f.a.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.ConnectDue(ctx); err != nil {
			t.Fatal(err)
		}
	}

	suspended, active := models.PartnerStatusSuspended, models.PartnerStatusActive
	if _, xerr := f.svc.UpdatePartner(ctx, f.a.ID, UpdatePartnerInput{Status: &suspended}); xerr != nil {
		t.Fatal(xerr)
	}
	due()
	if len(del.delegated) != 0 || len(del.purchased) != 0 {
		t.Fatalf("a suspended partner's mailboxes connected: %v %v", del.delegated, del.purchased)
	}
	if _, xerr := f.svc.UpdatePartner(ctx, f.a.ID, UpdatePartnerInput{Status: &active}); xerr != nil {
		t.Fatal(xerr)
	}
	due()
	if v, ok := del.delegated["bob@proven.test"]; !ok || v {
		t.Fatalf("proven mailbox connect = %v %v, want unvouched under its grant", v, ok)
	}
	if v, ok := del.delegated["amy@infra.test"]; !ok || !v {
		t.Fatalf("allowed-domain mailbox connect = %v %v, want vouched", v, ok)
	}
	if !slices.Equal(del.purchased, []string{"ada@getacme.test"}) {
		t.Fatalf("brokered connects = %v", del.purchased)
	}
}
