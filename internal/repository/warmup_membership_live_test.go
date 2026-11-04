package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// One query returns each participant with what decides its pool and every pool it sits in.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveParticipantMemberships -v
func TestLiveParticipantMembershipsReadEveryParticipantAtOnce(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	user, org, active, paused := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tag := "membership-" + org.String()[:8]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Pool', 'Live', $2, 'x')`, user, tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Pool', $2, $3)`, org, tag, user)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{active, paused})
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user)
	})
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, warmup_pool_type)
	      VALUES ($1, $2, $3, $4, 'A', '', '', 'gmail', 'premium')`, active, user, org, tag+"[EMAIL]")
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status, warmup_pool_type)
	      VALUES ($1, $2, $3, $4, 'B', '', '', 'gmail', 'inactive', NULL)`, paused, user, org, tag+"[EMAIL]")
	exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id) VALUES ($1, $2), ($3, $4)`,
		models.WarmupPoolFreeID, active, models.WarmupPoolPremiumID, paused)

	members, err := NewWarmupRepository(handle.Pool).ListParticipantMemberships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]WarmupParticipantMembership{}
	for _, m := range members {
		got[m.EmailAccountID] = m
	}
	a, b := got[active], got[paused]
	if a.Status != "active" || a.OrganizationID == nil || *a.OrganizationID != org || a.WarmupPoolType != "premium" || len(a.PoolTypes) != 1 || a.PoolTypes[0] != "free" {
		t.Fatalf("active mailbox read as %+v", a)
	}
	if b.Status != "inactive" || b.WarmupPoolType != "" || len(b.PoolTypes) != 1 || b.PoolTypes[0] != "premium" {
		t.Fatalf("paused mailbox read as %+v", b)
	}
}
