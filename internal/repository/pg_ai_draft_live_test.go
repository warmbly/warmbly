package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// UpsertPendingDraftForThread writes the reply draft an external caller generated.
//
// Run against a local PostgreSQL:
//
//	WARMBLY_TEST_DB=postgres://warmbly@127.0.0.1:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUpsertThreadDraft -v

type threadDraftFixture struct {
	warmupUsageFixture
	pool *pgxpool.Pool
	repo AIDraftRepository
}

func newThreadDraftFixture(t *testing.T, pool *pgxpool.Pool) *threadDraftFixture {
	t.Helper()
	f := &threadDraftFixture{warmupUsageFixture: newWarmupUsageFixture(t, pool), pool: pool, repo: NewAIDraftRepository(pool)}
	t.Cleanup(func() {
		f.exec(`DELETE FROM ai_thread_drafts WHERE organization_id = $1`, f.org)
		f.exec(`DELETE FROM unibox_emails WHERE email_id = $1`, f.account)
		f.exec(`DELETE FROM campaign_contact_progress WHERE contact_id IN (SELECT id FROM contacts WHERE organization_id = $1)`, f.org)
		f.exec(`DELETE FROM sequences WHERE organization_id = $1`, f.org)
		f.exec(`DELETE FROM campaigns WHERE organization_id = $1`, f.org)
		f.exec(`DELETE FROM contacts WHERE organization_id = $1`, f.org)
	})
	return f
}

// mailbox is the fixture mailbox's own address.
func (f *threadDraftFixture) mailbox(t *testing.T) string {
	t.Helper()
	var email string
	if err := f.pool.QueryRow(context.Background(), `SELECT email FROM email_accounts WHERE id = $1`, f.account).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// message stores one mailbox message and returns its id.
func (f *threadDraftFixture) message(thread, folder, from, subject string, at time.Time) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO unibox_emails (id, user_id, email_id, thread_id, message_id, from_addr, to_addr, subject, folder, internal_date)
	        VALUES ($1, $2, $3, $4, $5, ARRAY[$6]::text[], ARRAY['mailbox@example.test']::text[], $7, $8, $9)`,
		id, f.user, f.account, thread, "<"+id.String()+"@lead.test>", from, subject, folder, at)
	return id
}

func (f *threadDraftFixture) drafts(t *testing.T, thread string) (n int, status string) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COALESCE(MIN(status), '') FROM ai_thread_drafts WHERE organization_id = $1 AND thread_id = $2`,
		f.org, thread).Scan(&n, &status)
	if err != nil {
		t.Fatal(err)
	}
	return n, status
}

func draftParams(thread, body string) UpsertDraftParams {
	return UpsertDraftParams{ThreadID: thread, Body: body, Model: "llcdrop", IntentClass: "pricing", Confidence: 0.7}
}

func TestLiveUpsertThreadDraft(t *testing.T) {
	pool := liveCampaignReplayDB(t)
	ctx := context.Background()
	base := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)

	t.Run("insert fills the row like the inbox agent and a second call replaces the body", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		contact, campaign, sequence := uuid.New(), uuid.New(), uuid.New()
		f.exec(`INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields) VALUES($1,$2,$3,'lead@example.test','','','','','{}')`, contact, f.user, f.org)
		f.exec(`INSERT INTO campaigns(id,user_id,organization_id,name,description,status,days,timezone,created_at,updated_at) VALUES($1,$2,$3,'C','','active',127,'UTC',NOW(),NOW())`, campaign, f.user, f.org)
		f.exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position) VALUES($1,$2,$3,'S','S','T','',0,1)`, sequence, campaign, f.org)
		f.exec(`INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,sent_at) VALUES($1,$2,$3,$4)`, campaign, contact, sequence, base)
		older := f.message("th-1", "inbox", "Old <lead@example.test>", "Quote", base)
		newest := f.message("th-1", "inbox", "Lead <LEAD@Example.test>", "Quote", base.Add(time.Hour))
		// Our own copies and junk never count as the inbound message.
		own := f.mailbox(t)
		f.message("th-1", "sent", own, "Re: Quote", base.Add(2*time.Hour))
		f.message("th-1", "spam", "bot@example.test", "Quote", base.Add(3*time.Hour))
		f.message("th-1", "inbox", "Me <"+own+">", "Re: Quote", base.Add(4*time.Hour))

		d, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-1", "first"))
		if err != nil || !created {
			t.Fatalf("created=%v err=%v", created, err)
		}
		if d.EmailAccountID != f.account || d.OwnerUserID != f.user || d.ToAddr != "lead@example.test" ||
			d.Subject != "Re: Quote" || d.Status != "pending" || d.Model != "llcdrop" || d.IntentClass != "pricing" || d.Confidence != 0.7 {
			t.Fatalf("draft = %+v", d)
		}
		if d.SourceMessageID == nil || *d.SourceMessageID != newest || *d.SourceMessageID == older {
			t.Fatalf("source = %v, want newest inbound %s", d.SourceMessageID, newest)
		}
		if d.InReplyTo != "<"+newest.String()+"@lead.test>" {
			t.Fatalf("in_reply_to = %q", d.InReplyTo)
		}
		if d.ContactID == nil || *d.ContactID != contact || d.CampaignID == nil || *d.CampaignID != campaign {
			t.Fatalf("contact/campaign = %v/%v", d.ContactID, d.CampaignID)
		}

		again, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, UpsertDraftParams{ThreadID: "th-1", Body: "second", Model: "llcdrop", IntentClass: "meeting", Confidence: 0.2})
		if err != nil || created || again.ID != d.ID {
			t.Fatalf("replace: created=%v id=%s want %s err=%v", created, again.ID, d.ID, err)
		}
		got, err := f.repo.GetDraft(ctx, f.org, d.ID)
		if err != nil || got.Body != "second" || got.IntentClass != "meeting" || got.Confidence != 0.2 || got.Model != "llcdrop" || !got.UpdatedAt.After(got.CreatedAt) {
			t.Fatalf("stored draft = %+v err=%v", got, err)
		}
		if n, _ := f.drafts(t, "th-1"); n != 1 {
			t.Fatalf("%d drafts, want 1", n)
		}
	})

	t.Run("no contact leaves the links empty", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-2", "inbox", "stranger@example.test", "", base)
		d, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-2", "hi"))
		if err != nil || !created || d.ContactID != nil || d.CampaignID != nil || d.Subject != "Re:" {
			t.Fatalf("draft=%+v created=%v err=%v", d, created, err)
		}
	})

	t.Run("a thread keyed by the message id when its thread id is empty", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		id := f.message("", "inbox", "lead@example.test", "Hello", base)
		d, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(id.String(), "hi"))
		if err != nil || d.SourceMessageID == nil || *d.SourceMessageID != id {
			t.Fatalf("draft=%+v err=%v", d, err)
		}
	})

	t.Run("unknown thread and another organization's thread are not found", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		other := newThreadDraftFixture(t, pool)
		other.message("th-3", "inbox", "lead@example.test", "Hi", base)
		for name, c := range map[string]struct {
			org    uuid.UUID
			thread string
		}{"unknown": {f.org, "nope"}, "foreign": {f.org, "th-3"}, "only outbound": {f.org, "th-out"}} {
			if name == "only outbound" {
				f.message("th-out", "sent", f.mailbox(t), "Hi", base)
			}
			if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, c.org, draftParams(c.thread, "x")); !errors.Is(err, ErrDraftThreadNotFound) {
				t.Fatalf("%s: err = %v", name, err)
			}
		}
		if n, _ := other.drafts(t, "th-3"); n != 0 {
			t.Fatal("a foreign org's call wrote a draft")
		}
	})

	t.Run("an approved or discarded draft on the newest inbound message is never touched", func(t *testing.T) {
		for _, tc := range []struct {
			status string
			want   error
		}{{"approved", ErrDraftAlreadySent}, {"discarded", ErrDraftDiscarded}} {
			f := newThreadDraftFixture(t, pool)
			f.message("th-4", "inbox", "lead@example.test", "Hi", base)
			d, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-4", "original"))
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := f.repo.SetDraftStatus(ctx, f.org, d.ID, tc.status); err != nil || !ok {
				t.Fatalf("set %s: %v %v", tc.status, ok, err)
			}
			if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-4", "changed")); !errors.Is(err, tc.want) {
				t.Fatalf("%s: err = %v, want %v", tc.status, err, tc.want)
			}
			got, _ := f.repo.GetDraft(ctx, f.org, d.ID)
			if got.Body != "original" || got.Status != tc.status {
				t.Fatalf("%s draft changed: %+v", tc.status, got)
			}
			if n, _ := f.drafts(t, "th-4"); n != 1 {
				t.Fatalf("%s: %d drafts, want 1", tc.status, n)
			}
		}
	})

	t.Run("a new inbound reply after an approved draft gets a fresh pending draft", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-5", "inbox", "lead@example.test", "Hi", base)
		first, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-5", "one"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.SetDraftStatus(ctx, f.org, first.ID, "approved"); err != nil {
			t.Fatal(err)
		}
		next := f.message("th-5", "inbox", "lead@example.test", "Re: Hi", time.Now().UTC().Add(time.Hour))
		d, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-5", "two"))
		if err != nil || !created || d.ID == first.ID || *d.SourceMessageID != next {
			t.Fatalf("draft=%+v created=%v err=%v", d, created, err)
		}
		if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-5", "three")); err != nil {
			t.Fatalf("second call on the new draft: %v", err)
		}
		if n, _ := f.drafts(t, "th-5"); n != 2 {
			t.Fatalf("%d drafts, want 2", n)
		}
	})

	t.Run("a pending draft on an older message is retargeted at the newest inbound message", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		contact := uuid.New()
		f.exec(`INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields) VALUES($1,$2,$3,'second@example.test','','','','','{}')`, contact, f.user, f.org)
		old := f.message("th-6", "inbox", "first@example.test", "Quote", base)
		first, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-6", "one"))
		if err != nil || !created || *first.SourceMessageID != old {
			t.Fatalf("first: %+v created=%v err=%v", first, created, err)
		}
		// The newest message arrived in a second mailbox of the workspace.
		other := uuid.New()
		f.exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, warmup_max)
		        VALUES ($1, $2, $3, $4, 'Other', '', '', 'smtp_imap', 40)`, other, f.user, f.org, "other-"+uuid.NewString()+"@example.test")
		t.Cleanup(func() {
			f.exec(`DELETE FROM ai_thread_drafts WHERE organization_id = $1`, f.org)
			f.exec(`DELETE FROM unibox_emails WHERE email_id = $1`, other)
			f.exec(`DELETE FROM email_accounts WHERE id = $1`, other)
		})
		newest := uuid.New()
		f.exec(`INSERT INTO unibox_emails (id, user_id, email_id, thread_id, message_id, from_addr, to_addr, subject, folder, internal_date)
		        VALUES ($1, $2, $3, 'th-6', $4, ARRAY['Second <second@example.test>']::text[], ARRAY['other@example.test']::text[], 'Re: Quote (final)', 'inbox', $5)`,
			newest, f.user, other, "<"+newest.String()+"@lead.test>", base.Add(time.Hour))

		d, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-6", "two"))
		if err != nil || created || d.ID == first.ID {
			t.Fatalf("retarget kept its id: id=%s first %s created=%v err=%v", d.ID, first.ID, created, err)
		}
		// Whoever reviewed the old version holds its id, and approving it finds nothing to send.
		if stale, err := f.repo.ClaimDraftForApproval(ctx, f.org, first.ID, nil); err != nil || stale != nil {
			t.Fatalf("claimed the version answering the old message: %+v err=%v", stale, err)
		}
		got, err := f.repo.GetDraft(ctx, f.org, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Body != "two" || got.SourceMessageID == nil || *got.SourceMessageID != newest ||
			got.ToAddr != "second@example.test" || got.Subject != "Re: Quote (final)" ||
			got.InReplyTo != "<"+newest.String()+"@lead.test>" || got.ContactID == nil || *got.ContactID != contact ||
			got.EmailAccountID != other {
			t.Fatalf("draft still answers the old message: %+v", got)
		}
		if n, _ := f.drafts(t, "th-6"); n != 1 {
			t.Fatalf("%d drafts, want 1", n)
		}
	})

	t.Run("a pending draft on an older message is left alone when a resolved draft covers the newest", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-7", "inbox", "lead@example.test", "Hi", base)
		first, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-7", "pending"))
		if err != nil {
			t.Fatal(err)
		}
		newest := f.message("th-7", "inbox", "lead@example.test", "Re: Hi", base.Add(time.Hour))
		f.exec(`INSERT INTO ai_thread_drafts (id, organization_id, email_account_id, owner_user_id, thread_id, source_message_id,
		            to_addr, subject, in_reply_to, body, intent_class, confidence, model, status, created_at, updated_at)
		        VALUES ($1,$2,$3,$4,'th-7',$5,'lead@example.test','Re: Hi','','sent','',0,'agent','approved',NOW(),NOW())`,
			uuid.New(), f.org, f.account, f.user, newest)

		if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-7", "changed")); !errors.Is(err, ErrDraftAlreadySent) {
			t.Fatalf("err = %v, want ErrDraftAlreadySent", err)
		}
		got, _ := f.repo.GetDraft(ctx, f.org, first.ID)
		if got.Body != "pending" || *got.SourceMessageID == newest {
			t.Fatalf("pending draft changed: %+v", got)
		}
	})

	t.Run("mailbox allowlist", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-6", "inbox", "lead@example.test", "Hi", base)
		p := draftParams("th-6", "x")
		p.AllowedMailboxes = []uuid.UUID{uuid.New()}
		if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, p); !errors.Is(err, ErrDraftMailboxNotAllowed) {
			t.Fatalf("outside allowlist: %v", err)
		}
		if n, _ := f.drafts(t, "th-6"); n != 0 {
			t.Fatal("a refused call wrote a draft")
		}
		p.AllowedMailboxes = []uuid.UUID{f.account}
		d, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, p)
		if err != nil {
			t.Fatalf("inside allowlist: %v", err)
		}
		p.AllowedMailboxes = []uuid.UUID{uuid.New()}
		if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, p); !errors.Is(err, ErrDraftMailboxNotAllowed) {
			t.Fatalf("pending draft outside allowlist: %v", err)
		}
		if got, _ := f.repo.GetDraft(ctx, f.org, d.ID); got.Body != "x" {
			t.Fatalf("refused call changed the body: %q", got.Body)
		}
	})

	t.Run("concurrent calls leave one pending draft", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-7", "inbox", "lead@example.test", "Hi", base)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var created int
		errs := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, c, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-7", "race"))
				if err != nil {
					errs <- err
					return
				}
				if c {
					mu.Lock()
					created++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent upsert: %v", err)
		}
		if n, status := f.drafts(t, "th-7"); n != 1 || status != "pending" || created != 1 {
			t.Fatalf("drafts=%d status=%q created=%d, want 1 pending draft from 1 insert", n, status, created)
		}
	})
}

// lockDraft holds the draft's row lock in its own transaction until release is called.
func (f *threadDraftFixture) lockDraft(t *testing.T, id uuid.UUID) (release func()) {
	t.Helper()
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `SELECT 1 FROM ai_thread_drafts WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	return func() { _ = tx.Rollback(context.Background()) }
}

// waitForLockWaiters blocks until n sessions in this database wait on a lock.
func (f *threadDraftFixture) waitForLockWaiters(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := f.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lock waiters", n)
}

func (f *threadDraftFixture) pendingCount(t *testing.T, thread string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ai_thread_drafts WHERE organization_id = $1 AND thread_id = $2 AND status = 'pending'`,
		f.org, thread).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// sameSend reports whether two rows send the same mail.
func sameSend(a, b *models.AIThreadDraft) bool {
	return a.ID == b.ID && a.Body == b.Body && a.ToAddr == b.ToAddr && a.Subject == b.Subject &&
		a.InReplyTo == b.InReplyTo && a.EmailAccountID == b.EmailAccountID && a.Status == b.Status &&
		(a.SourceMessageID == nil) == (b.SourceMessageID == nil) &&
		(a.SourceMessageID == nil || *a.SourceMessageID == *b.SourceMessageID)
}

type upsertResult struct {
	draft   *models.AIThreadDraft
	created bool
	err     error
}

type claimResult struct {
	draft *models.AIThreadDraft
	err   error
}

func TestLiveApproveThreadDraft(t *testing.T) {
	pool := liveCampaignReplayDB(t)
	ctx := context.Background()
	base := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)

	// pendingOnOlder leaves a pending draft "old" on one message and a newer inbound message from someone else.
	pendingOnOlder := func(t *testing.T, f *threadDraftFixture, thread string) (draft *models.AIThreadDraft, older, newer uuid.UUID) {
		t.Helper()
		older = f.message(thread, "inbox", "first@lead.test", "Quote", base)
		d, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(thread, "old"))
		if err != nil {
			t.Fatal(err)
		}
		newer = f.message(thread, "inbox", "Second <second@lead.test>", "Re: Quote", base.Add(time.Hour))
		return d, older, newer
	}

	t.Run("an approval queued behind an upsert claims the replaced body, and nothing once the draft is retargeted", func(t *testing.T) {
		for _, retarget := range []bool{false, true} {
			f := newThreadDraftFixture(t, pool)
			thread := "th-a-" + uuid.NewString()[:8]
			var d *models.AIThreadDraft
			var newer uuid.UUID
			if retarget {
				d, _, newer = pendingOnOlder(t, f, thread)
			} else {
				newer = f.message(thread, "inbox", "[EMAIL]", "Quote", base)
				var err error
				if d, _, err = f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(thread, "old")); err != nil {
					t.Fatal(err)
				}
			}
			release := f.lockDraft(t, d.ID)
			up := make(chan upsertResult, 1)
			go func() {
				r, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(thread, "new"))
				up <- upsertResult{r, created, err}
			}()
			f.waitForLockWaiters(t, 1)
			cl := make(chan claimResult, 1)
			go func() {
				r, err := f.repo.ClaimDraftForApproval(ctx, f.org, d.ID, nil)
				cl <- claimResult{r, err}
			}()
			f.waitForLockWaiters(t, 2)
			release()

			u, c := <-up, <-cl
			if u.err != nil || u.created || *u.draft.SourceMessageID != newer || (u.draft.ID != d.ID) == !retarget {
				t.Fatalf("retarget=%v upsert: %+v created=%v err=%v", retarget, u.draft, u.created, u.err)
			}
			if c.err != nil {
				t.Fatalf("retarget=%v claim: %v", retarget, c.err)
			}
			if retarget {
				// The reviewer approved the version answering the older message: nothing is sent.
				if c.draft != nil {
					t.Fatalf("claimed a retargeted draft through its old id: %+v", c.draft)
				}
				if n := f.pendingCount(t, thread); n != 1 {
					t.Fatalf("%d pending drafts, want the retargeted one", n)
				}
				continue
			}
			if c.draft == nil || c.draft.Body != "new" || c.draft.Status != "approved" {
				t.Fatalf("claim returned a version the upsert replaced: %+v", c.draft)
			}
			stored, _ := f.repo.GetDraft(ctx, f.org, d.ID)
			if !sameSend(stored, c.draft) {
				t.Fatalf("stored %+v differs from claimed %+v", stored, c.draft)
			}
			if n := f.pendingCount(t, thread); n != 0 {
				t.Fatalf("%d pending drafts, want 0", n)
			}
		}
	})

	t.Run("an upsert queued behind an approval never changes the claimed row", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			newer   bool
			want    error
			pending int
		}{
			{"same newest message is already sent", false, ErrDraftAlreadySent, 0},
			{"a newer message gets its own pending draft", true, nil, 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newThreadDraftFixture(t, pool)
				thread := "th-b-" + uuid.NewString()[:8]
				older := f.message(thread, "inbox", "first@lead.test", "Quote", base)
				d, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(thread, "old"))
				if err != nil {
					t.Fatal(err)
				}
				var newer uuid.UUID
				if tc.newer {
					newer = f.message(thread, "inbox", "first@lead.test", "Re: Quote", base.Add(time.Hour))
				}
				release := f.lockDraft(t, d.ID)
				cl := make(chan claimResult, 1)
				go func() {
					r, err := f.repo.ClaimDraftForApproval(ctx, f.org, d.ID, nil)
					cl <- claimResult{r, err}
				}()
				f.waitForLockWaiters(t, 1)
				up := make(chan upsertResult, 1)
				go func() {
					r, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(thread, "new"))
					up <- upsertResult{r, created, err}
				}()
				f.waitForLockWaiters(t, 2)
				release()

				c, u := <-cl, <-up
				if c.err != nil || c.draft == nil || c.draft.Body != "old" || *c.draft.SourceMessageID != older {
					t.Fatalf("claim: %+v err=%v", c.draft, c.err)
				}
				if !errors.Is(u.err, tc.want) {
					t.Fatalf("upsert err = %v, want %v", u.err, tc.want)
				}
				if tc.newer && (!u.created || u.draft.ID == d.ID || *u.draft.SourceMessageID != newer) {
					t.Fatalf("upsert after the claim: %+v created=%v", u.draft, u.created)
				}
				stored, _ := f.repo.GetDraft(ctx, f.org, d.ID)
				if !sameSend(stored, c.draft) {
					t.Fatalf("approved row changed after the claim: stored %+v, claimed %+v", stored, c.draft)
				}
				if n := f.pendingCount(t, thread); n != tc.pending {
					t.Fatalf("%d pending drafts, want %d", n, tc.pending)
				}
			})
		}
	})

	t.Run("concurrent approvals and upserts keep the claimed snapshot", func(t *testing.T) {
		for i := range 20 {
			f := newThreadDraftFixture(t, pool)
			thread := "th-c-" + uuid.NewString()[:8]
			d, _, _ := pendingOnOlder(t, f, thread)
			var wg sync.WaitGroup
			claims := make(chan claimResult, 2)
			ups := make(chan upsertResult, 2)
			for range 2 {
				wg.Add(2)
				go func() {
					defer wg.Done()
					r, err := f.repo.ClaimDraftForApproval(ctx, f.org, d.ID, nil)
					claims <- claimResult{r, err}
				}()
				go func() {
					defer wg.Done()
					r, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams(thread, "new"))
					ups <- upsertResult{r, created, err}
				}()
			}
			wg.Wait()
			close(claims)
			close(ups)
			var won []*models.AIThreadDraft
			for c := range claims {
				if c.err != nil {
					t.Fatalf("iteration %d claim: %v", i, c.err)
				}
				if c.draft != nil {
					won = append(won, c.draft)
				}
			}
			if len(won) > 1 {
				t.Fatalf("iteration %d: %d approvals claimed the draft", i, len(won))
			}
			for u := range ups {
				if u.err != nil && !errors.Is(u.err, ErrDraftAlreadySent) {
					t.Fatalf("iteration %d upsert: %v", i, u.err)
				}
			}
			stored, _ := f.repo.GetDraft(ctx, f.org, d.ID)
			// Either an approval claimed the version it was shown, or a retarget moved the draft to a new id first.
			if len(won) == 1 && (won[0].Body != "old" || !sameSend(stored, won[0])) {
				t.Fatalf("iteration %d: stored %+v, claimed %+v", i, stored, won[0])
			}
			if len(won) == 0 && (stored != nil || f.pendingCount(t, thread) != 1) {
				t.Fatalf("iteration %d: nothing claimed, yet the old id is %+v", i, stored)
			}
			if n := f.pendingCount(t, thread); n > 1 {
				t.Fatalf("iteration %d: %d pending drafts", i, n)
			}
		}
	})

	t.Run("late-arriving inbound mail after an approved draft gets a fresh pending draft", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-d", "inbox", "first@lead.test", "Hi", base)
		first, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-d", "one"))
		if err != nil {
			t.Fatal(err)
		}
		if c, err := f.repo.ClaimDraftForApproval(ctx, f.org, first.ID, nil); err != nil || c == nil {
			t.Fatalf("claim: %v %v", c, err)
		}
		// Synced after the approval but dated before it.
		late := f.message("th-d", "inbox", "first@lead.test", "Re: Hi", base.Add(time.Hour))
		d, created, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-d", "two"))
		if err != nil || !created || d.ID == first.ID || *d.SourceMessageID != late || d.Status != "pending" {
			t.Fatalf("draft=%+v created=%v err=%v", d, created, err)
		}
		if got, _ := f.repo.GetDraft(ctx, f.org, first.ID); got.Body != "one" || got.Status != "approved" {
			t.Fatalf("approved draft changed: %+v", got)
		}
	})

	t.Run("a resolved legacy draft with no source message covers mail that arrived before it", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-e", "inbox", "first@lead.test", "Hi", base)
		f.exec(`INSERT INTO ai_thread_drafts (id, organization_id, email_account_id, owner_user_id, thread_id,
		            to_addr, subject, in_reply_to, body, intent_class, confidence, model, status, created_at, updated_at)
		        VALUES ($1,$2,$3,$4,'th-e','first@lead.test','Re: Hi','','sent','',0,'agent','approved',NOW(),NOW())`,
			uuid.New(), f.org, f.account, f.user)
		if _, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-e", "x")); !errors.Is(err, ErrDraftAlreadySent) {
			t.Fatalf("err = %v, want ErrDraftAlreadySent", err)
		}
	})

	t.Run("a claim is held to the organization and the mailbox allowlist", func(t *testing.T) {
		f := newThreadDraftFixture(t, pool)
		f.message("th-f", "inbox", "first@lead.test", "Hi", base)
		d, _, err := f.repo.UpsertPendingDraftForThread(ctx, f.org, draftParams("th-f", "x"))
		if err != nil {
			t.Fatal(err)
		}
		for name, c := range map[string]struct {
			org     uuid.UUID
			allowed []uuid.UUID
		}{"foreign org": {uuid.New(), nil}, "outside allowlist": {f.org, []uuid.UUID{uuid.New()}}, "empty allowlist": {f.org, []uuid.UUID{}}} {
			if got, err := f.repo.ClaimDraftForApproval(ctx, c.org, d.ID, c.allowed); err != nil || got != nil {
				t.Fatalf("%s: claimed %+v err=%v", name, got, err)
			}
		}
		if n := f.pendingCount(t, "th-f"); n != 1 {
			t.Fatal("a refused claim resolved the draft")
		}
		got, err := f.repo.ClaimDraftForApproval(ctx, f.org, d.ID, []uuid.UUID{f.account})
		if err != nil || got == nil || got.Body != "x" {
			t.Fatalf("inside allowlist: %+v err=%v", got, err)
		}
		if again, err := f.repo.ClaimDraftForApproval(ctx, f.org, d.ID, nil); err != nil || again != nil {
			t.Fatalf("second claim: %+v err=%v", again, err)
		}
	})
}
