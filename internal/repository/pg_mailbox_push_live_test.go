package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func livePushDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	return handle
}

type pushFixture struct {
	user, org                    uuid.UUID
	gmail, outlook, onOldWorker  uuid.UUID
	wakeWorker, oldWorker        uuid.UUID
	gmailAddress, outlookAddress string
}

func newPushFixture(t *testing.T, handle *db.DB) *pushFixture {
	t.Helper()
	ctx := context.Background()
	f := &pushFixture{user: uuid.New(), org: uuid.New(), gmail: uuid.New(), outlook: uuid.New(), onOldWorker: uuid.New(),
		wakeWorker: uuid.New(), oldWorker: uuid.New()}
	tag := "push-" + f.org.String()[:8]
	f.gmailAddress, f.outlookAddress = tag+"[EMAIL]", tag+"[EMAIL]"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := handle.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	nodes, workers := NewFleetNodeRepository(handle), NewWorkerRepository(handle.Pool)
	for id, protocol := range map[uuid.UUID]int{f.wakeWorker: 1, f.oldWorker: 0} {
		if err := nodes.UpsertOnHeartbeat(ctx, models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleWorker, SyncWakeProtocol: protocol}); err != nil {
			t.Fatalf("heartbeat: %v", err)
		}
		if err := workers.EnsureWorkerRow(ctx, id); err != nil {
			t.Fatalf("worker row: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Push', 'Live', $2, 'x')`, f.user, tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Push', $2, $3)`, f.org, tag, f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, worker_id)
	      VALUES ($1, $2, $3, $4, 'Gmail', '', '', 'gmail', $5)`, f.gmail, f.user, f.org, strings.ToUpper(f.gmailAddress[:1])+f.gmailAddress[1:], f.wakeWorker)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, worker_id)
	      VALUES ($1, $2, $3, $4, 'Outlook', '', '', 'outlook', $5)`, f.outlook, f.user, f.org, f.outlookAddress, f.wakeWorker)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, worker_id)
	      VALUES ($1, $2, $3, $4, 'Old', '', '', 'outlook', $5)`, f.onOldWorker, f.user, f.org, tag+"[EMAIL]", f.oldWorker)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = handle.Pool.Exec(c, `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{f.gmail, f.outlook, f.onOldWorker})
		_, _ = handle.Pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, f.org)
		_, _ = handle.Pool.Exec(c, `DELETE FROM users WHERE id = $1`, f.user)
		_, _ = handle.Pool.Exec(c, `DELETE FROM workers WHERE id = ANY($1)`, []uuid.UUID{f.wakeWorker, f.oldWorker})
		_, _ = handle.Pool.Exec(c, `DELETE FROM fleet_nodes WHERE id = ANY($1)`, []uuid.UUID{f.wakeWorker, f.oldWorker})
	})
	return f
}

func TestLiveMailboxPushChannelsRoundTripAndScopeToTheOwner(t *testing.T) {
	handle := livePushDB(t)
	f := newPushFixture(t, handle)
	repo := NewMailboxPushRepository(handle)
	ctx := context.Background()
	expires := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Microsecond)

	if err := repo.PutPushState(ctx, uuid.New(), f.outlook, &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "x", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetPushState(ctx, f.outlook); err != nil || got != nil {
		t.Fatalf("another user's report was stored: %+v, %v", got, err)
	}
	want := &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "sub-1", ExpiresAt: expires, Credential: "c1"}
	if err := repo.PutPushState(ctx, f.user, f.outlook, want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetPushState(ctx, f.outlook)
	if err != nil || got == nil || got.SubscriptionID != "sub-1" || got.Credential != "c1" || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if err := repo.PutPushState(ctx, f.user, f.outlook, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetPushState(ctx, f.outlook); got != nil {
		t.Fatalf("a closed channel survived: %+v", got)
	}
}

func TestLiveWakeTargetsSkipWorkersThatCannotActOnAWake(t *testing.T) {
	handle := livePushDB(t)
	f := newPushFixture(t, handle)
	repo := NewMailboxPushRepository(handle)
	ctx := context.Background()

	targets, err := repo.WakeTargets(ctx, []uuid.UUID{f.outlook, f.onOldWorker})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].EmailID != f.outlook || targets[0].WorkerID != f.wakeWorker || targets[0].UserID != f.user {
		t.Fatalf("targets %+v, want only the mailbox on the wake-capable worker", targets)
	}

	if got, err := repo.GmailWakeTargets(ctx, f.gmailAddress); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Fatalf("a Gmail mailbox with no watch was woken: %+v", got)
	}
	if err := repo.PutPushState(ctx, f.user, f.gmail, &models.MailboxPushState{Provider: models.MailboxPushGmail, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GmailWakeTargets(ctx, strings.ToUpper(f.gmailAddress))
	if err != nil || len(got) != 1 || got[0].EmailID != f.gmail {
		t.Fatalf("a watched Gmail mailbox was not found by its address in another case: %+v, %v", got, err)
	}
}
