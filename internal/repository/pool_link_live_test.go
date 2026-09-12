package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The pool link's two queries that a second reader of the same mailbox depends
// on. An enrolled mailbox is synced by the instance AND by Warmbly Cloud, so
// whichever consumes the token first must not decide what the other sees.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LivePoolLink -v

type poolLinkFixture struct {
	pool      *pgxpool.Pool
	user      uuid.UUID
	org       uuid.UUID
	recipient uuid.UUID
	sender    uuid.UUID
	senderTo  string
	task      uuid.UUID
	warmup    WarmupRepository
}

func newPoolLinkFixture(t *testing.T) *poolLinkFixture {
	t.Helper()
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := &poolLinkFixture{
		pool: pool, user: uuid.New(), org: uuid.New(),
		recipient: uuid.New(), sender: uuid.New(), task: uuid.New(),
		warmup: NewWarmupRepository(pool),
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name, password_hash) VALUES ($1, $2, 'Pool', 'Link', 'x')`,
		f.user, "pl-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Pool Link', $2, $3)`,
		f.org, "pl-"+f.org.String()[:8], f.user)
	for _, id := range []uuid.UUID{f.recipient, f.sender} {
		addr := "pl-" + id.String()[:8] + "@test.local"
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		          signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'PL', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
			id, f.user, f.org, addr)
		if id == f.sender {
			f.senderTo = addr
		}
	}
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
	      VALUES ($1, 'warmup', $2, 'completed', '')`, f.task, f.sender)
	t.Cleanup(func() {
		c := context.Background()
		for _, sql := range []string{
			`DELETE FROM warmup_received WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM warmup_tokens WHERE recipient_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM tasks WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM email_accounts WHERE organization_id = $1`,
			`DELETE FROM organizations WHERE id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			if _, err := pool.Exec(c, sql, f.org); err != nil {
				t.Errorf("cleanup %q: %v", sql[:min(50, len(sql))], err)
			}
		}
	})
	return f
}

// token writes one warmup token addressed to the recipient.
func (f *poolLinkFixture) token(t *testing.T, messageID, subject string, consumed bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var consumedAt any
	if consumed {
		consumedAt = time.Now()
	}
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, sent_message_id, subject, consumed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, f.task, f.sender, f.recipient, messageID, subject, consumedAt)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	return id
}

// A mailbox Warmbly Cloud warms is read by the cloud and by the instance. The
// cloud consumes the token when it syncs first, and the instance's question
// ("is this yours?") must still answer yes, or the cloud's warmup mail lands in
// the owner's unibox.
func TestLivePoolLinkWarmupDeliverySurvivesAConsumedToken(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	const messageID = "<pool-link-consumed@test.local>"
	f.token(t, messageID, "Quick question", true)

	found, err := f.warmup.FindDeliveredWarmupToken(ctx, f.recipient, f.senderTo, messageID, "Quick question")
	if err != nil {
		t.Fatalf("FindDeliveredWarmupToken: %v", err)
	}
	if found != nil {
		t.Fatal("a consumed token should not be resolvable for local verification")
	}

	ok, err := f.warmup.IsWarmupDelivery(ctx, f.recipient, f.senderTo, messageID, "Quick question")
	if err != nil {
		t.Fatalf("IsWarmupDelivery: %v", err)
	}
	if !ok {
		t.Fatal("consumed warmup mail must still be recognised as warmup for a linked instance")
	}
}

func TestLivePoolLinkWarmupDeliveryMatchesAndRefuses(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()

	// Pending token, matched on the sender/subject pair rather than the id.
	f.token(t, "", "Following up", false)
	ok, err := f.warmup.IsWarmupDelivery(ctx, f.recipient, f.senderTo, "", "Following up")
	if err != nil {
		t.Fatalf("IsWarmupDelivery pair: %v", err)
	}
	if !ok {
		t.Fatal("a pending token for this sender and subject should match")
	}

	// A delivery already recorded, whose token has since been cleaned up.
	const receivedID = "<pool-link-received@test.local>"
	if err := f.warmup.RecordWarmupReceived(ctx, f.recipient, uuid.New(), receivedID, f.sender); err != nil {
		t.Fatalf("RecordWarmupReceived: %v", err)
	}
	ok, err = f.warmup.IsWarmupDelivery(ctx, f.recipient, "someone@elsewhere.test", receivedID, "Anything")
	if err != nil {
		t.Fatalf("IsWarmupDelivery received: %v", err)
	}
	if !ok {
		t.Fatal("a recorded warmup delivery should be recognised by its message id")
	}

	// Real mail from someone else must never be dropped.
	ok, err = f.warmup.IsWarmupDelivery(ctx, f.recipient, "prospect@elsewhere.test", "<real-mail@elsewhere.test>", "Are you free Thursday?")
	if err != nil {
		t.Fatalf("IsWarmupDelivery stranger: %v", err)
	}
	if ok {
		t.Fatal("ordinary mail must not be taken for warmup")
	}
}

// An approved code the instance never came back for must not keep a usable
// bearer token in plaintext once it expires.
func TestLivePoolLinkExpiredCodeLosesItsToken(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewPoolLinkRepository(pool)
	id := uuid.New()
	suffix := id.String()[:8]

	if _, err := pool.Exec(ctx,
		`INSERT INTO pool_link_codes (id, device_code_hash, user_code, instance_name, status, instance_token, expires_at)
		 VALUES ($1, $2, $3, 'Abandoned', 'approved', 'wpl_secret', NOW() - INTERVAL '1 minute')`,
		id, "hash-"+suffix, "PL"+suffix[:2]+"-"+suffix[2:6]); err != nil {
		t.Fatalf("insert code: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM pool_link_codes WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup code: %v", err)
		}
	})

	if err := repo.DeleteExpiredCodes(ctx); err != nil {
		t.Fatalf("DeleteExpiredCodes: %v", err)
	}
	var token *string
	if err := pool.QueryRow(ctx, `SELECT instance_token FROM pool_link_codes WHERE id = $1`, id).Scan(&token); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if token != nil {
		t.Fatalf("expired code kept its plaintext instance token: %q", *token)
	}
}
