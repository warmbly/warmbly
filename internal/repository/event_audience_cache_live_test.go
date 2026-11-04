package repository

import (
	"context"
	"testing"
)

func TestLiveEventAudienceRemembersResourceOrganizationsOnly(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	audience := NewEventAudienceRepository(pool)
	if org, err := audience.ResourceOrganization(ctx, "campaign", f.campaign); err != nil || org != f.org {
		t.Fatalf("resource org=%v err=%v", org, err)
	}
	// The second lookup is served from memory: it answers through a reader whose pool is closed.
	_, other := liveContactDB(t)
	closed := NewEventAudienceRepository(other)
	closed.orgs = audience.orgs
	other.Close()
	if org, err := closed.ResourceOrganization(ctx, "campaign", f.campaign); err != nil || org != f.org {
		t.Fatalf("cached resource org=%v err=%v", org, err)
	}
	if _, err := closed.CanAddressUser(ctx, f.org, f.mate); err == nil {
		t.Fatal("membership was answered without asking the database")
	}
}
