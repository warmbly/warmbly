package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func TestLiveManagedCloudOperationIsCorrelatedIdempotentAndNeverRevived(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	r := NewPoolLinkRepository(f.pool)
	instance := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org, CreatedBy: &f.user, URL: "https://instance.test", Name: "Fixture"}
	if err := r.CreateInstance(ctx, instance, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET provider = 'gmail', auth_method = 'oauth' WHERE id = $1`, f.sender); err != nil {
		t.Fatal(err)
	}
	mr := r.(PoolLinkManagedRepository)
	remote, hash := uuid.New(), crypt.SHA256("never-store-the-browser-handle")
	op := &models.PoolLinkManagedOperation{OrganizationID: f.org, InstanceID: &instance.ID, RemoteID: &remote,
		SessionHash: &hash, Kind: "oauth", Provider: models.InboxProviderGoogle, PlannedAccountID: &f.sender, ExpiresAt: time.Now().Add(time.Minute)}
	for range 2 {
		if err := mr.CreateManagedOperation(ctx, op); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := mr.ClaimManagedOperation(ctx, instance.ID, remote)
	if err != nil || !claimed {
		t.Fatalf("claim=%v, err=%v", claimed, err)
	}
	if again, err := mr.ClaimManagedOperation(ctx, instance.ID, remote); err != nil || again {
		t.Fatalf("double callback claim=%v, err=%v", again, err)
	}
	for range 2 {
		if err := mr.CompleteManagedOperation(ctx, instance.ID, remote, f.sender); err != nil {
			t.Fatal(err)
		}
	}
	// A new repository object has no process-local state to recover.
	got, err := NewPoolLinkRepository(f.pool).(PoolLinkManagedRepository).GetManagedOperation(ctx, instance.ID, remote)
	if err != nil || got == nil || got.State != "completed" || got.CompletedAt == nil || got.AccountID == nil || *got.AccountID != f.sender {
		t.Fatalf("result=%+v, err=%v", got, err)
	}
	if other, err := mr.GetManagedOperation(ctx, uuid.New(), remote); err != nil || other != nil {
		t.Fatalf("cross instance result=%+v, err=%v", other, err)
	}
	if err := mr.RevokeManagedOperations(ctx, instance.ID, &remote); err != nil {
		t.Fatal(err)
	}
	if err := mr.CompleteManagedOperation(ctx, instance.ID, remote, f.sender); err == nil {
		t.Fatal("revoked operation revived")
	}
	if err := r.DeleteMailbox(ctx, instance.ID, remote); err != nil {
		t.Fatal(err)
	}
	if err := mr.CreateManagedOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	if claimed, err := mr.ClaimManagedOperation(ctx, instance.ID, remote); err != nil || claimed {
		t.Fatalf("revocation replay claim=%v,err=%v", claimed, err)
	}
	// A deletion arriving before a delayed start reserves a restrictive tombstone.
	lateRemote := uuid.New()
	if err := mr.RevokeManagedOperations(ctx, instance.ID, &lateRemote); err != nil {
		t.Fatal(err)
	}
	op.RemoteID = &lateRemote
	if err := mr.CreateManagedOperation(ctx, op); err == nil {
		t.Fatal("delayed start replaced tombstone")
	}
	if m, err := r.GetMailboxByRemote(ctx, instance.ID, remote); err != nil || m != nil {
		t.Fatalf("recreated mailbox=%+v,err=%v", m, err)
	}
}

func TestLiveManagedCloudOperationExpiryAndInstanceRevocationFailClosed(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	r := NewPoolLinkRepository(f.pool)
	i := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org, Name: "Fixture"}
	if err := r.CreateInstance(ctx, i, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET provider = 'gmail' WHERE id = $1`, f.sender); err != nil {
		t.Fatal(err)
	}
	mr := r.(PoolLinkManagedRepository)
	for _, expired := range []bool{true, false} {
		remote := uuid.New()
		expiry := time.Now().Add(time.Minute)
		if expired {
			expiry = time.Now().Add(-time.Minute)
		}
		op := &models.PoolLinkManagedOperation{OrganizationID: f.org, InstanceID: &i.ID, RemoteID: &remote, Kind: "adopt", Provider: models.InboxProviderGoogle, PlannedAccountID: &f.sender, ExpiresAt: expiry}
		if err := mr.CreateManagedOperation(ctx, op); err != nil {
			t.Fatal(err)
		}
		if !expired {
			if err := r.RevokeInstance(ctx, i.ID); err != nil {
				t.Fatal(err)
			}
		}
		if err := mr.CompleteManagedOperation(ctx, i.ID, remote, f.sender); err == nil {
			t.Fatal("expired/revoked operation completed")
		}
	}
}

func TestLiveManagedLocalConsentRetainsActorAuthorityAndPortableRestriction(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	instance, remote, planned, hash := uuid.New(), uuid.New(), uuid.New(), crypt.SHA256("opaque-session")
	if _, err := f.pool.Exec(ctx, `INSERT INTO cloud_link (cloud_url,instance_id,token) VALUES ('https://cloud.test',$1,'fixture')`, instance); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(ctx, `DELETE FROM cloud_link WHERE instance_id = $1`, instance) })
	r := NewCloudLinkRepository(f.pool, nil).(CloudManagedConsentRepository)
	c := &models.CloudManagedConsent{ID: uuid.New(), OrganizationID: f.org, UserID: &f.user, InstanceID: &instance, RemoteID: &remote, PlannedAccountID: &planned, SessionHash: &hash, Kind: "oauth", Provider: models.InboxProviderGoogle, ExpiresAt: time.Now().Add(time.Minute)}
	if err := r.CreateManagedConsent(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := NewCloudLinkRepository(f.pool, nil).(CloudManagedConsentRepository).GetManagedConsent(ctx, f.org, f.user, hash)
	if err != nil || got == nil || got.State != "pending" || got.SessionHash == nil || *got.SessionHash != hash {
		t.Fatalf("restart=%+v,err=%v", got, err)
	}
	if other, err := r.GetManagedConsent(ctx, uuid.New(), f.user, hash); err != nil || other != nil {
		t.Fatal("cross workspace consent found")
	}
	if other, err := r.GetManagedConsent(ctx, f.org, uuid.New(), hash); err != nil || other != nil {
		t.Fatal("cross actor consent found")
	}
	cloud := uuid.New()
	if err := r.BindManagedCloudAccount(ctx, f.org, c.ID, cloud); err != nil {
		t.Fatal(err)
	}
	if err := r.BindManagedCloudAccount(ctx, f.org, c.ID, uuid.New()); err == nil {
		t.Fatal("cloud account identity was replaced")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE organizations SET risk_state = 'restricted' WHERE id = $1`, f.org); err != nil {
		t.Fatal(err)
	}
	if allowed, err := r.ManagedConsentAuthorized(ctx, got); err != nil || allowed {
		t.Fatalf("revoked org authority=%v,err=%v", allowed, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE organizations SET risk_state = 'trusted' WHERE id = $1`, f.org); err != nil {
		t.Fatal(err)
	}
	if err := r.RevokeManagedConsents(ctx, instance, &planned); err != nil {
		t.Fatal(err)
	}
	if err := r.SetManagedConsentState(ctx, f.org, c.ID, "active", nil); err == nil {
		t.Fatal("late acknowledgement reversed revocation")
	}
	if err := r.SetManagedConsentState(ctx, f.org, c.ID, "revoked", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE cloud_managed_consents SET instance_id=NULL,remote_id=NULL,session_hash=NULL,planned_account_id=NULL WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	got, err = r.GetManagedConsent(ctx, f.org, f.user, hash)
	if err != nil || got != nil {
		t.Fatal("reset import handles remained resumable")
	}
}

func TestLiveManagedTokenUsesCurrentConsentAndObservedStanding(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	instance, remote, hash := uuid.New(), uuid.New(), crypt.SHA256("fixture-current-consent")
	if _, err := f.pool.Exec(ctx, `INSERT INTO cloud_link (cloud_url,instance_id,token) VALUES ('https://cloud.test',$1,'fixture')`, instance); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(ctx, `DELETE FROM cloud_link WHERE instance_id = $1`, instance) })
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET provider='gmail',auth_method='oauth' WHERE id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	base := NewCloudLinkRepository(f.pool, nil)
	r := base.(CloudManagedConsentRepository)
	c := &models.CloudManagedConsent{ID: uuid.New(), OrganizationID: f.org, UserID: &f.user, InstanceID: &instance, RemoteID: &remote, SessionHash: &hash, PlannedAccountID: &f.sender, Kind: "oauth", Provider: models.InboxProviderGoogle, ExpiresAt: time.Now().Add(time.Minute)}
	if err := r.CreateManagedConsent(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := r.BindManagedCloudAccount(ctx, f.org, c.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetManagedConsentState(ctx, f.org, c.ID, "active", &f.sender); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Enroll(ctx, f.sender, remote, instance, true); err != nil {
		t.Fatal(err)
	}
	assertAllowed := func(want bool) {
		t.Helper()
		allowed, err := r.CanBrokerManagedToken(ctx, f.sender)
		if err != nil || allowed != want {
			t.Fatalf("allowed=%v,want=%v,err=%v", allowed, want, err)
		}
	}
	assertAllowed(false)
	if _, err := base.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "healthy"}, false); err != nil {
		t.Fatal(err)
	}
	assertAllowed(true)
	if _, err := f.pool.Exec(ctx, `UPDATE cloud_link_mailboxes SET standing_observed_at=now()-interval '16 minutes' WHERE email_account_id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	assertAllowed(false)
	if _, err := base.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "healthy"}, false); err != nil {
		t.Fatal(err)
	}
	if err := r.RevokeManagedConsents(ctx, instance, &f.sender); err != nil {
		t.Fatal(err)
	}
	assertAllowed(false)
}
