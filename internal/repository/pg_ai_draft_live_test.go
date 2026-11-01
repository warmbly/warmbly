package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
		if err != nil || created || d.ID != first.ID {
			t.Fatalf("retarget: id=%s want %s created=%v err=%v", d.ID, first.ID, created, err)
		}
		got, err := f.repo.GetDraft(ctx, f.org, first.ID)
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
