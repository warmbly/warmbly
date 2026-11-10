package repository

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// The agency home reads every client with one query per metric, selected by an
// organization list. Run against a fresh, migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAgencyHome -v

func TestAgencyHomeQueriesSelectByOrganizationList(t *testing.T) {
	for name, query := range map[string]string{
		"campaigns": agencyCampaignStatsSQL,
		"mailboxes": agencyMailboxStatsSQL,
		"replies":   agencyRepliesWaitingSQL,
		"advisor":   agencyAdvisorCountsSQL,
	} {
		if !strings.Contains(query, "ANY($1::uuid[])") && !strings.Contains(query, "unnest($1::uuid[])") {
			t.Errorf("%s does not select by the organization list", name)
		}
		if strings.Contains(query, "organization_id = $1 ") || strings.Contains(query, "organization_id = $1\n") {
			t.Errorf("%s compares against a single organization", name)
		}
	}

	q, args := agencyInboxQuery(models.AgencyInboxQuery{OrgIDs: []uuid.UUID{uuid.New()}, Limit: 10})
	if len(args) != 4 || !strings.Contains(q, "ea.organization_id = ANY($1::uuid[])") ||
		!strings.Contains(q, "ea.organization_id = ANY($2::uuid[]) AND ea.id = ANY($3::uuid[])") {
		t.Fatalf("inbox tenant predicate missing: %d args", len(args))
	}
	if strings.Contains(q, "(b.internal_date, b.id) <") {
		t.Fatal("first page carries a keyset clause")
	}
	q, args = agencyInboxQuery(models.AgencyInboxQuery{OrgIDs: []uuid.UUID{uuid.New()}, BeforeAt: time.Now(), BeforeID: uuid.New(), Limit: 10, Unread: true, NeedsReply: true, Positive: true, Search: "hello there"})
	if !strings.Contains(q, "(b.internal_date, b.id) < ($") || len(args) < 7 {
		t.Fatalf("keyset clause or filter args missing: %d args", len(args))
	}
}

func TestLiveAgencyHome(t *testing.T) {
	pool := liveCampaignReplayDB(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, query)
		}
	}
	user, owner := uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{user, owner} {
		exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'A', 'B', $2)`, u, u.String()+"@example.test")
	}
	agency := uuid.New()
	exec(`INSERT INTO agencies (id, name, owner_user_id, status) VALUES ($1, 'Agency', $2, 'active')`, agency, owner)

	acme, beta, foreign := uuid.New(), uuid.New(), uuid.New()
	mailbox := map[uuid.UUID]uuid.UUID{}
	for i, org := range []uuid.UUID{acme, beta, foreign} {
		exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, $2, $3)`, org, []string{"Acme", "Beta", "Foreign"}[i], owner)
		box := uuid.New()
		mailbox[org] = box
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, warmup_tag)
			VALUES ($1, $2, $3, $4, 'Box', '', '', 'smtp_imap', 'tag')`, box, owner, org, box.String()+"@client.test")
	}
	exec(`INSERT INTO agency_clients (organization_id, agency_id) VALUES ($1, $3), ($2, $3)`, acme, beta, agency)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions) VALUES ($1, $2, 'admin', 255)`, acme, user)
	exec(`UPDATE email_accounts SET status = 'inactive' WHERE id = $1`, mailbox[acme])

	// Three conversations in Acme, one in the foreign workspace, all inbound.
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	addMsg := func(org uuid.UUID, thread string, at time.Time, seen bool) uuid.UUID {
		id := uuid.New()
		exec(`INSERT INTO unibox_emails (id, user_id, email_id, folder, thread_id, message_id, from_addr, to_addr, subject, snippet, internal_date, sent_date, seen)
			VALUES ($1, $2, $3, 'inbox', $4, $5, ARRAY['Lead <lead@prospect.test>'], ARRAY['box@client.test'], 'Hello', 'Hi', $6, $6, $7)`,
			id, owner, mailbox[org], thread, "<"+id.String()+"@x>", at, seen)
		return id
	}
	addMsg(acme, "t1", base, true)
	addMsg(acme, "t1", base.Add(time.Minute), false)
	addMsg(acme, "t2", base.Add(2*time.Minute), true)
	addMsg(acme, "t3", base.Add(3*time.Minute), false)
	addMsg(foreign, "t9", base.Add(4*time.Minute), false)

	r := NewAgencyHomeRepository(pool)
	members, err := r.ClientMemberships(ctx, agency, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].OrganizationID != acme || !members[0].IsMember() || members[1].IsMember() {
		t.Fatalf("memberships: %+v", members)
	}

	orgs := []uuid.UUID{acme, beta}
	if _, err := r.CampaignStats(ctx, orgs, time.Now().Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	boxes, err := r.MailboxStats(ctx, orgs)
	if err != nil {
		t.Fatal(err)
	}
	if boxes[acme].Total != 1 || boxes[acme].Disconnected != 1 || boxes[beta].Total != 1 || boxes[foreign] != nil {
		t.Fatalf("mailboxes: %+v %+v", boxes[acme], boxes[beta])
	}
	waiting, err := r.RepliesWaiting(ctx, orgs)
	if err != nil {
		t.Fatal(err)
	}
	if waiting[acme] != 2 || waiting[foreign] != 0 {
		t.Fatalf("replies waiting: %v", waiting)
	}
	if _, err := r.AdvisorCounts(ctx, orgs); err != nil {
		t.Fatal(err)
	}

	// Two per page, newest first, never the foreign workspace's mail.
	var seen []string
	q := models.AgencyInboxQuery{OrgIDs: orgs, Limit: 2}
	for range 3 {
		rows, err := r.InboxThreads(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		page := rows
		if len(page) > q.Limit {
			page = page[:q.Limit]
		}
		for _, th := range page {
			if th.OrganizationID != acme {
				t.Fatalf("thread from %s outside the scope", th.OrganizationID)
			}
			seen = append(seen, th.ThreadID)
		}
		if len(rows) <= q.Limit {
			break
		}
		last := page[len(page)-1]
		q.BeforeAt, q.BeforeID = last.InternalDate, last.ID
	}
	if !slices.Equal(seen, []string{"t3", "t2", "t1"}) {
		t.Fatalf("pages: %v", seen)
	}

	rows, err := r.InboxThreads(ctx, models.AgencyInboxQuery{OrgIDs: orgs, Unread: true, NeedsReply: true, Positive: false, Search: "hello", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("unread needing a reply: %+v", rows)
	}
	if _, err := r.InboxThreads(ctx, models.AgencyInboxQuery{OrgIDs: orgs, Positive: true, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	// A restricted membership reads only granted mailboxes, and only in its own workspace.
	rows, err = r.InboxThreads(ctx, models.AgencyInboxQuery{ScopedOrgIDs: []uuid.UUID{beta}, ScopedMailboxes: []uuid.UUID{mailbox[acme], mailbox[foreign]}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a grant outside its workspace was read: %+v", rows)
	}
}
