package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// The reply opt-out recheck reads the entries a reply opt-out wrote, the
// sender's mail inside the workspace only, and never reads an entry twice.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveReplyOptOutRecheck -v
func TestLiveReplyOptOutRecheckQueries(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	mailbox, stranger := uuid.New(), uuid.New()
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Recheck', '', '', 'smtp_imap')`, mailbox, f.owner, f.org, "recheck-"+mailbox.String()[:6]+"@test.local")
	sender := "news-" + uuid.New().String()[:6] + "@example.org"
	inbox := func(from, folder string) uuid.UUID {
		id := uuid.New()
		exec(`INSERT INTO unibox_emails (id, user_id, email_id, folder, provider_folder, from_addr, subject, body_text, in_reply_to, flags)
		      VALUES ($1, $2, $3, $4, $4, ARRAY[$5], 'Weekly', 'Read more.
Unsubscribe here.', ARRAY['<a@example.test>'], ARRAY['List-Id:<news.example.org>'])`, id, f.owner, mailbox, folder, from)
		return id
	}
	received := inbox("News <"+sender+">", "inbox")
	inbox("IMAP Form ("+sender+")", "inbox")
	sentCopy := inbox("Us <us@test.local>", "sent")
	exec(`UPDATE unibox_emails SET to_addr = ARRAY[$2] WHERE id = $1`, sentCopy, "News <"+sender+">")
	inbox("Someone <someone@example.org>", "inbox")
	// Another workspace's mailbox holding mail from the same sender.
	otherOrg := uuid.New()
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Other', $2, $3)`, otherOrg, "recheck-"+otherOrg.String()[:8], f.owner)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Other', '', '', 'smtp_imap')`, stranger, f.owner, otherOrg, "other-"+stranger.String()[:6]+"@test.local")
	exec(`INSERT INTO unibox_emails (id, user_id, email_id, folder, provider_folder, from_addr)
	      VALUES ($1, $2, $3, 'inbox', 'inbox', ARRAY[$4])`, uuid.New(), f.owner, stranger, sender)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM unibox_emails WHERE email_id = ANY($1)`, []uuid.UUID{mailbox, stranger})
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{mailbox, stranger})
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, otherOrg)
		_, _ = pool.Exec(context.Background(), `DELETE FROM suppressed_recipients WHERE organization_id = $1`, f.org)
	})

	unibox := &uniboxRepository{db: handle}
	msgs, err := unibox.ListInboundFrom(ctx, f.org, sender, 10)
	if err != nil {
		t.Fatalf("ListInboundFrom: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("ListInboundFrom returned %d messages, want the 2 inbound ones in this workspace", len(msgs))
	}
	var found bool
	for _, m := range msgs {
		if m.ID == received {
			found = true
			if m.BodyText != "Read more.\nUnsubscribe here." || len(m.InReplyTo) != 1 || len(m.Flags) != 1 || m.CreatedAt.IsZero() || !m.ProcessedAt.Equal(m.CreatedAt) {
				t.Errorf("message not read whole: %+v", m)
			}
		}
	}
	if !found {
		t.Error("the inbound message was not returned")
	}

	// A sent copy to the address is what outlives a deleted contact.
	written, err := unibox.HasWrittenTo(ctx, f.org, sender)
	if err != nil || !written {
		t.Fatalf("HasWrittenTo(sender) = %v, %v; want true from the sent copy", written, err)
	}
	if written, err := unibox.HasWrittenTo(ctx, f.org, "someone@example.org"); err != nil || written {
		t.Fatalf("HasWrittenTo(an address only received from) = %v, %v; want false", written, err)
	}
	if written, err := unibox.HasWrittenTo(ctx, uuid.New(), sender); err != nil || written {
		t.Fatalf("HasWrittenTo in another workspace = %v, %v; want false", written, err)
	}

	adv := &advancedOutreachRepository{db: pool}
	reply := models.SuppressedRecipient{OrganizationID: f.org, Email: sender, Reason: "asked to stop in a reply",
		Source: models.DeliverabilityEventUnsubscribe, Metadata: map[string]interface{}{"via": "reply"}}
	link := models.SuppressedRecipient{OrganizationID: f.org, Email: "link-" + sender, Reason: "clicked the unsubscribe link",
		Source: models.DeliverabilityEventUnsubscribe, Metadata: map[string]interface{}{"via": "link"}}
	for _, e := range []models.SuppressedRecipient{reply, link} {
		if err := adv.UpsertSuppressedRecipient(ctx, &e); err != nil {
			t.Fatalf("UpsertSuppressedRecipient: %v", err)
		}
	}
	ours := func() []models.SuppressedRecipient {
		t.Helper()
		all, err := adv.ListUncheckedReplyOptOuts(ctx, uuid.Nil, 10000)
		if err != nil {
			t.Fatalf("ListUncheckedReplyOptOuts: %v", err)
		}
		var out []models.SuppressedRecipient
		for _, e := range all {
			if e.OrganizationID == f.org {
				out = append(out, e)
			}
		}
		return out
	}
	got := ours()
	if len(got) != 1 || got[0].Email != sender {
		t.Fatalf("unchecked reply opt-outs = %+v, want only the reply entry", got)
	}
	if err := adv.MarkReplyOptOutChecked(ctx, got[0].ID, "kept"); err != nil {
		t.Fatalf("MarkReplyOptOutChecked: %v", err)
	}
	if left := ours(); len(left) != 0 {
		t.Fatalf("a checked entry was listed again: %+v", left)
	}
	var via string
	if err := pool.QueryRow(ctx, `SELECT metadata->>'via' FROM suppressed_recipients WHERE id = $1`, got[0].ID).Scan(&via); err != nil || via != "reply" {
		t.Fatalf("marking dropped the entry's metadata: via=%q err=%v", via, err)
	}

	// The delete only takes the row it read: a newer write, or a row a link
	// wrote, is left in place.
	read := got[0]
	if ok, err := adv.DeleteReplyOptOut(ctx, f.org, read.ID, read.UpdatedAt.Add(-time.Second)); err != nil || ok {
		t.Fatalf("DeleteReplyOptOut on a rewritten row = %v, %v; want it kept", ok, err)
	}
	var linkRow models.SuppressedRecipient
	if err := pool.QueryRow(ctx, `SELECT id, updated_at FROM suppressed_recipients WHERE organization_id = $1 AND email = $2`,
		f.org, "link-"+sender).Scan(&linkRow.ID, &linkRow.UpdatedAt); err != nil {
		t.Fatalf("read link row: %v", err)
	}
	if ok, err := adv.DeleteReplyOptOut(ctx, f.org, linkRow.ID, linkRow.UpdatedAt); err != nil || ok {
		t.Fatalf("DeleteReplyOptOut on a link unsubscribe = %v, %v; want it kept", ok, err)
	}
	if ok, err := adv.DeleteReplyOptOut(ctx, f.org, read.ID, read.UpdatedAt); err != nil || !ok {
		t.Fatalf("DeleteReplyOptOut on the row as read = %v, %v; want it removed", ok, err)
	}
}
