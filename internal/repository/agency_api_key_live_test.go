package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Agency keys are read by hash only while live and their creator unbanned, and every other read is held to its agency.
func TestLiveAgencyAPIKeyLifecycle(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 376)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	owner, agency, other := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Agency', 'Live', $2, 'x')`, owner, "agk-"+owner.String()[:8]+"@test.local")
	exec(`INSERT INTO agencies (id, name, owner_user_id) VALUES ($1, 'Live agency', $3), ($2, 'Other agency', $3)`, agency, other, owner)

	repo := NewAgencyAPIKeyRepository(pool)
	mk := func(name, hash string, expires *time.Time) *models.AgencyAPIKey {
		t.Helper()
		k, err := repo.Create(ctx, agency, owner, &models.CreateAgencyAPIKey{Name: name, Permissions: models.APIPermAgencyRead, AllowedIPs: []string{"10.0.0.0/8"}, ExpiresAt: expires}, "wmagency_ab", "wxyz", hash)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return k
	}
	live := mk("live", "hash-live-"+agency.String(), nil)
	past := time.Now().Add(-time.Hour)
	mk("expired", "hash-expired-"+agency.String(), &past)
	third := mk("third", "hash-third-"+agency.String(), nil)

	if k, err := repo.GetActiveByHash(ctx, "hash-live-"+agency.String()); err != nil || k == nil || k.ID != live.ID || k.Permissions != models.APIPermAgencyRead || k.RateLimitPerMinute != 60 {
		t.Fatalf("live key by hash = %+v, %v", k, err)
	}
	if k, _ := repo.GetActiveByHash(ctx, "hash-expired-"+agency.String()); k != nil {
		t.Error("an expired key authenticated")
	}
	if k, _ := repo.Get(ctx, other, live.ID); k != nil {
		t.Error("a key was read through another agency")
	}
	if k, _ := repo.Update(ctx, other, live.ID, &models.UpdateAgencyAPIKey{AllowedIPs: []string{}}); k != nil {
		t.Error("a key was changed through another agency")
	}

	page, err := repo.List(ctx, agency, 2, nil, uuid.Nil)
	if err != nil || len(page) != 2 || page[0].ID != third.ID {
		t.Fatalf("first page = %d keys, %v", len(page), err)
	}
	rest, err := repo.List(ctx, agency, 2, &page[1].CreatedAt, page[1].ID)
	if err != nil || len(rest) != 1 || rest[0].ID != live.ID {
		t.Fatalf("second page = %+v, %v", rest, err)
	}

	name := "renamed"
	if k, err := repo.Update(ctx, agency, live.ID, &models.UpdateAgencyAPIKey{Name: &name}); err != nil || k.Name != name || len(k.AllowedIPs) != 1 {
		t.Fatalf("rename = %+v, %v", k, err)
	}
	if k, err := repo.Update(ctx, agency, live.ID, &models.UpdateAgencyAPIKey{AllowedIPs: []string{}}); err != nil || len(k.AllowedIPs) != 0 || k.Name != name {
		t.Fatalf("clear allowlist = %+v, %v", k, err)
	}
	if err := repo.TouchLastUsed(ctx, live.ID, "192.0.2.4"); err != nil {
		t.Fatalf("touch: %v", err)
	}

	exec(`UPDATE users SET ban_scope = 1 WHERE id = $1`, owner)
	if k, _ := repo.GetActiveByHash(ctx, "hash-live-"+agency.String()); k != nil {
		t.Error("a banned creator's key authenticated")
	}
	exec(`UPDATE users SET ban_scope = 0 WHERE id = $1`, owner)

	for i := 0; i < 2; i++ {
		if ok, err := repo.Revoke(ctx, agency, live.ID, "test"); err != nil || !ok {
			t.Fatalf("revoke %d = %v, %v", i, ok, err)
		}
	}
	if ok, _ := repo.Revoke(ctx, other, third.ID, "test"); ok {
		t.Error("a key was revoked through another agency")
	}
	if k, _ := repo.GetActiveByHash(ctx, "hash-live-"+agency.String()); k != nil {
		t.Error("a revoked key authenticated")
	}
	if n, err := repo.RevokeCreatedBy(ctx, owner, "password"); err != nil || n != 2 { // third and the expired one
		t.Errorf("revoke created by = %d, %v", n, err)
	}
	exec(`DELETE FROM agencies WHERE id IN ($1, $2)`, agency, other)
	exec(`DELETE FROM users WHERE id = $1`, owner)
}
