package repository

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Inbox tagging reads the campaign a reply belongs to as a fact. On Gmail the
// Message-ID on the task can differ from the one the sent copy syncs back
// with, so the lookup also goes through the thread handle and In-Reply-To.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveInboxTagCampaign -v

func (f *threadParentFixture) unibox(mailbox uuid.UUID, folder, messageID, threadID, from string, inReplyTo []string, at time.Time) {
	if inReplyTo == nil {
		inReplyTo = []string{}
	}
	f.exec(`INSERT INTO unibox_emails (id, user_id, email_id, folder, provider_folder, message_id, thread_id,
	            from_addr, subject, body_text, in_reply_to, internal_date)
	        VALUES ($1, $2, $3, $4, $4, $5, $6, ARRAY[$7::text], 'Re: Hello', 'body of '||$5, $8, $9)`,
		uuid.New(), f.owner, mailbox, folder, messageID, threadID, from, inReplyTo, at)
}

func (f *threadParentFixture) verdict(messageID, threadID, kind string, labels []string) {
	f.exec(`INSERT INTO inbox_tag_results (organization_id, email_account_id, message_id, thread_id, status, kind, labels)
	        VALUES ($1, $2, $3, $4, 'complete', $5, $6)`, f.org, f.mailbox, messageID, threadID, kind, labels)
}

func newInboxTagCampaignFixture(t *testing.T) *threadParentFixture {
	t.Helper()
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	t.Cleanup(func() {
		c := context.Background()
		for _, sql := range []string{
			`DELETE FROM unibox_thread_labels WHERE organization_id = $1`,
			`DELETE FROM categories WHERE organization_id = $1`,
			`DELETE FROM inbox_tag_results WHERE organization_id = $1`,
			`DELETE FROM unibox_emails WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
		} {
			if _, err := pool.Exec(c, sql, f.org); err != nil {
				t.Errorf("cleanup %q: %v", sql, err)
			}
		}
	})
	return f
}

// The task keeps the Message-ID we minted while the synced sent copy carries
// the one Gmail stamped; the thread handle still names the campaign.
func TestLiveInboxTagCampaignResolvesThroughTheGmailThread(t *testing.T) {
	f := newInboxTagCampaignFixture(t)
	ctx := context.Background()
	step := f.step(0, "Hello", true)
	f.send(step, f.mailbox, "<minted@gmail.com>", "gthr-1", 600)
	now := time.Now().UTC()
	f.unibox(f.mailbox, "sent", "<CABstamped@mail.gmail.com>", "gthr-1", "me@test.local", nil, now.Add(-10*time.Minute))

	repo := NewInboxTagRepository(f.pool)
	body, campaign, err := repo.PreviousOutbound(ctx, f.mailbox, "gthr-1", nil, now)
	if err != nil {
		t.Fatalf("previous outbound: %v", err)
	}
	if campaign != "Thread Parent" || body != "body of <CABstamped@mail.gmail.com>" {
		t.Fatalf("got body %q campaign %q", body, campaign)
	}

	// Another mailbox holding a send in a thread of the same id is not ours.
	_, campaign, err = repo.PreviousOutbound(ctx, f.other, "gthr-1", nil, now)
	if err != nil || campaign != "" {
		t.Fatalf("another mailbox resolved %q (%v)", campaign, err)
	}
}

// With no thread to go on, a Message-ID the reply names is enough, in either
// bracket form.
func TestLiveInboxTagCampaignResolvesThroughInReplyTo(t *testing.T) {
	f := newInboxTagCampaignFixture(t)
	step := f.step(0, "Hello", true)
	f.send(step, f.mailbox, "<sent-2@test.local>", "", 600)

	repo := NewInboxTagRepository(f.pool)
	_, campaign, err := repo.PreviousOutbound(context.Background(), f.mailbox, "unrelated-thread", []string{"sent-2@test.local"}, time.Now())
	if err != nil {
		t.Fatalf("previous outbound: %v", err)
	}
	if campaign != "Thread Parent" {
		t.Fatalf("campaign %q, want the one the reply names", campaign)
	}
	if _, campaign, _ := repo.PreviousOutbound(context.Background(), f.mailbox, "unrelated-thread", nil, time.Now()); campaign != "" {
		t.Fatalf("no facts resolved %q", campaign)
	}
}

// The recheck lists cold_inbound verdicts in campaign threads and nothing else;
// Reopen hands one back to the untagged list.
func TestLiveInboxTagRecheckListsColdInboundInCampaignThreads(t *testing.T) {
	f := newInboxTagCampaignFixture(t)
	ctx := context.Background()
	step := f.step(0, "Hello", true)
	f.send(step, f.mailbox, "<minted@gmail.com>", "gthr-1", 600)
	now := time.Now().UTC()
	f.unibox(f.mailbox, "inbox", "<reply@lead.test>", "gthr-1", "Lead <lead@lead.test>", nil, now.Add(-5*time.Minute))
	f.unibox(f.mailbox, "inbox", "<pitch@vendor.test>", "vendor-thread", "Vendor <sales@vendor.test>", nil, now.Add(-4*time.Minute))
	f.verdict("<reply@lead.test>", "gthr-1", "cold_inbound", []string{"cold-inbound"})
	f.verdict("<pitch@vendor.test>", "vendor-thread", "cold_inbound", []string{"cold-inbound"})

	repo := NewInboxTagRepository(f.pool)
	got, err := repo.ListColdInboundInCampaignThreads(ctx, f.org, now.Add(-time.Hour), 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].MessageID != "<reply@lead.test>" {
		t.Fatalf("listed %+v, want only the reply in the campaign thread", got)
	}

	if labels, err := repo.Reopen(ctx, f.org, "<reply@lead.test>", "human_reply"); err != nil || labels != nil {
		t.Fatalf("reopen other kind: %v %v", labels, err)
	}
	if untagged, _ := repo.ListUntagged(ctx, f.org, now.Add(-time.Hour), 50); len(untagged) != 0 {
		t.Fatalf("a reopen for another kind dropped the verdict: %+v", untagged)
	}
	if labels, err := repo.Reopen(ctx, f.org, "<reply@lead.test>", "cold_inbound"); err != nil || !slices.Equal(labels, []string{"cold-inbound"}) {
		t.Fatalf("reopen: %v %v", labels, err)
	}
	untagged, err := repo.ListUntagged(ctx, f.org, now.Add(-time.Hour), 50)
	if err != nil {
		t.Fatalf("untagged: %v", err)
	}
	if len(untagged) != 1 || untagged[0].MessageID != "<reply@lead.test>" {
		t.Fatalf("untagged %+v, want the reopened reply", untagged)
	}
}

// An automatic label comes off only when no verdict in the thread still
// carries it, and a label a person applied never does. Titles match in any
// case, as categories do.
func TestLiveInboxTagRemoveAutoLabels(t *testing.T) {
	f := newInboxTagCampaignFixture(t)
	ctx := context.Background()
	store := NewTagCategoryStore(f.pool)
	cold, err := store.EnsureCategory(ctx, f.org, "Sales pitch")
	if err != nil {
		t.Fatalf("category: %v", err)
	}
	f.exec(`INSERT INTO unibox_thread_labels (organization_id, thread_id, category_id) VALUES ($1, 'auto-thread', $2)`, f.org, cold)
	f.exec(`INSERT INTO unibox_thread_labels (organization_id, thread_id, category_id, user_id) VALUES ($1, 'manual-thread', $2, $3)`, f.org, cold, f.owner)
	f.verdict("<other@lead.test>", "auto-thread", "cold_inbound", []string{"SALES PITCH"})

	labelled := func(thread string) bool {
		var n int
		if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM unibox_thread_labels WHERE organization_id = $1 AND thread_id = $2`, f.org, thread).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n > 0
	}

	if err := store.RemoveAutoLabels(ctx, f.org, "auto-thread", []string{"sales pitch"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !labelled("auto-thread") {
		t.Fatal("removed a label another verdict in the thread still carries")
	}
	f.exec(`UPDATE inbox_tag_results SET kind = 'human_reply', labels = ARRAY['Interested'] WHERE organization_id = $1`, f.org)
	if err := store.RemoveAutoLabels(ctx, f.org, "auto-thread", []string{"sales pitch"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if labelled("auto-thread") {
		t.Fatal("stale automatic label kept")
	}
	if err := store.RemoveAutoLabels(ctx, f.org, "manual-thread", []string{"sales pitch"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !labelled("manual-thread") {
		t.Fatal("removed a label a person applied")
	}
}
