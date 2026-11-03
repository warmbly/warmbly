package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Which conversations GET /unibox returns for a campaign_id or contact_id, and
// the campaign_id each row carries.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUniboxConversationScope -v
func TestLiveUniboxConversationScope(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newThreadParentFixture(t, handle.Pool)
	other := newThreadParentFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := t.Context()
	t.Cleanup(func() {
		_, _ = handle.Pool.Exec(context.Background(), `DELETE FROM unibox_emails WHERE email_id = $1`, f.mailbox)
	})

	var lead string
	if err := handle.Pool.QueryRow(ctx, `SELECT email FROM contacts WHERE id = $1`, f.contact).Scan(&lead); err != nil {
		t.Fatal(err)
	}
	step := f.step(0, "Hello", true)
	f.send(step, f.mailbox, "<first-send@test.local>", "", 200)
	f.send(step, f.mailbox, "second-send@test.local", "gmail-thread", 100)

	now := time.Now().UTC().Truncate(time.Second)
	n := 0
	message := func(thread, messageID, from string, inReplyTo []string, folder string) {
		t.Helper()
		n++
		at := now.Add(time.Duration(n) * time.Second)
		if err := repo.CreateEntry(ctx, f.owner, &models.EmailMessageStoreData{
			ID: uuid.New(), EmailID: f.mailbox, Folder: folder, ThreadID: thread, MessageID: messageID,
			FromAddr: []string{from}, ToAddr: []string{"[EMAIL]"}, InReplyTo: inReplyTo,
			Subject: "Scope", Snippet: "Scope", InternalDate: at, SentDate: at, CreatedAt: at, UpdatedAt: at,
		}); err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
	}
	// The send's own copy and a reply naming it, in one conversation.
	message("sent-thread", "<first-send@test.local>", "[EMAIL]", nil, models.FolderSent)
	message("sent-thread", "<reply-1@test.local>", "Lead <"+strings.ToUpper(lead)+">", []string{"<first-send@test.local>"}, models.FolderInbox)
	// A reply whose conversation holds nothing else, found by In-Reply-To alone.
	message("orphan-reply", "<reply-2@test.local>", lead, []string{"first-send@test.local"}, models.FolderInbox)
	// A later reply IMAP threaded on the send it descends from.
	message("<first-send@test.local>", "<reply-4@test.local>", lead, []string{"<reply-1@test.local>"}, models.FolderInbox)
	// A reply found by the mailbox's thread handle.
	message("gmail-thread", "<reply-3@test.local>", lead, nil, models.FolderInbox)
	// Mail with the contact outside any campaign.
	message("direct", "<direct@test.local>", "Lead <"+lead+">", nil, models.FolderInbox)
	// Somebody else.
	message("unrelated", "<unrelated@test.local>", "[EMAIL]", nil, models.FolderInbox)

	threads := func(p models.MailSearchParams) map[string]*uuid.UUID {
		t.Helper()
		p.PageSize = 50
		res, err := repo.Search(ctx, f.org, &p)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]*uuid.UUID{}
		for _, row := range res.Data {
			got[row.ThreadID] = row.CampaignID
		}
		return got
	}
	expect := func(name string, got map[string]*uuid.UUID, want map[string]bool) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: threads = %v, want %v", name, got, want)
		}
		for thread, inCampaign := range want {
			campaign, ok := got[thread]
			if !ok {
				t.Fatalf("%s: thread %q missing from %v", name, thread, got)
			}
			if matches := campaign != nil && *campaign == f.campaign; matches != inCampaign || (!inCampaign && campaign != nil) {
				t.Fatalf("%s: thread %q campaign_id = %v, want in campaign %v", name, thread, campaign, inCampaign)
			}
		}
	}

	rooted := "<first-send@test.local>"
	campaignThreads := map[string]bool{"sent-thread": true, "orphan-reply": true, "gmail-thread": true, rooted: true}
	expect("campaign", threads(models.MailSearchParams{CampaignID: &f.campaign}), campaignThreads)
	expect("campaign and contact", threads(models.MailSearchParams{CampaignID: &f.campaign, ContactID: &f.contact}), campaignThreads)
	expect("contact", threads(models.MailSearchParams{ContactID: &f.contact}),
		map[string]bool{"sent-thread": true, "orphan-reply": true, "gmail-thread": true, rooted: true, "direct": false})
	expect("unscoped", threads(models.MailSearchParams{}),
		map[string]bool{"sent-thread": true, "orphan-reply": true, "gmail-thread": true, rooted: true, "direct": false, "unrelated": false})
	expect("another workspace's campaign", threads(models.MailSearchParams{CampaignID: &other.campaign}), map[string]bool{})

	for _, tc := range []struct {
		name               string
		campaign, contact  *uuid.UUID
		wantCamp, wantCont bool
	}{
		{"own", &f.campaign, &f.contact, true, true},
		{"none", nil, nil, true, true},
		{"foreign campaign", &other.campaign, nil, false, true},
		{"foreign contact", nil, &other.contact, true, false},
	} {
		camp, cont, err := repo.SearchScopeOwned(ctx, f.org, tc.campaign, tc.contact)
		if err != nil {
			t.Fatal(err)
		}
		if camp != tc.wantCamp || cont != tc.wantCont {
			t.Fatalf("%s: SearchScopeOwned = %v, %v, want %v, %v", tc.name, camp, cont, tc.wantCamp, tc.wantCont)
		}
	}
}
