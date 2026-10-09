package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// Paging a keyset list to the end returns every row exactly once, whatever the
// page size. Run against a migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveListCursor -v
func TestLiveListCursorMailboxesReturnsEveryRowOnce(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	f := newAdminFixture(t, pool)

	want := map[string]bool{f.mailbox.String(): true}
	for i := 0; i < 11; i++ {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, created_at)
			VALUES ($1, $2, $3, $4, 'Paged', '', '', 'smtp_imap', NOW() - make_interval(mins => $5))`,
			id, f.user, f.org, fmt.Sprintf("%s-p%02d@test.local", f.tag, i), i+1); err != nil {
			t.Fatalf("insert mailbox: %v", err)
		}
		want[id.String()] = true
	}

	emails := NewEmailRepostory(handle, nil)
	for _, limit := range []int32{1, 5, 12, 50} {
		seen := map[string]int{}
		var cursor *string
		for page := 0; ; page++ {
			if page > len(want) {
				t.Fatalf("limit %d: paging did not end", limit)
			}
			res, xerr := emails.Search(ctx, f.org.String(), "", cursor, nil, limit, nil)
			if xerr != nil {
				t.Fatalf("limit %d: Search: %v", limit, xerr)
			}
			for _, e := range res.Data {
				seen[e.ID.String()]++
			}
			if !res.Pagination.HasMore {
				break
			}
			cursor, xerr = paging.DecodeCursor(*res.Pagination.NextCursor)
			if xerr != nil {
				t.Fatalf("limit %d: decode cursor: %v", limit, xerr)
			}
		}
		for id := range want {
			if seen[id] != 1 {
				t.Errorf("limit %d: mailbox %s returned %d times, want 1", limit, id, seen[id])
			}
		}
		if len(seen) != len(want) {
			t.Errorf("limit %d: %d mailboxes returned, want %d", limit, len(seen), len(want))
		}
	}
}

// drainUUIDPages pages a UUID-cursor list to the end and checks every wanted id came back once.
func drainUUIDPages(t *testing.T, name string, want map[uuid.UUID]bool, page func(cursor *uuid.UUID) ([]uuid.UUID, *string, bool)) {
	t.Helper()
	seen := map[uuid.UUID]int{}
	var cursor *uuid.UUID
	for n := 0; ; n++ {
		if n > len(want)+1 {
			t.Fatalf("%s: paging did not end", name)
		}
		ids, next, more := page(cursor)
		for _, id := range ids {
			seen[id]++
		}
		if !more {
			break
		}
		if next == nil {
			t.Fatalf("%s: has_more without a cursor", name)
		}
		id, err := paging.DecodeUUID(*next)
		if err != nil {
			t.Fatalf("%s: decode cursor: %v", name, err)
		}
		cursor = &id
	}
	for id := range want {
		if seen[id] != 1 {
			t.Errorf("%s: %s returned %d times, want 1", name, id, seen[id])
		}
	}
}

func TestLiveListCursorOtherListsReturnEveryRowOnce(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	f := newAdminFixture(t, pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, sql := range []string{
			`DELETE FROM crm_tasks WHERE organization_id = $1`,
			`DELETE FROM contact_notes WHERE organization_id = $1`,
			`DELETE FROM contacts WHERE organization_id = $1`,
			`DELETE FROM api_keys WHERE organization_id = $1`,
		} {
			if _, err := pool.Exec(c, sql, f.org); err != nil {
				t.Errorf("cleanup %q: %v", sql, err)
			}
		}
	})

	contact := uuid.New()
	exec(`INSERT INTO contacts (id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields)
	      VALUES ($1, $2, $3, 'Paged', 'Contact', $4, '', '', '{}')`, contact, f.user, f.org, f.tag+"-c@test.local")

	campaigns := map[uuid.UUID]bool{f.campaign: true}
	keys := map[uuid.UUID]bool{}
	notes := map[uuid.UUID]bool{}
	tasks := map[uuid.UUID]bool{}
	for i := 0; i < 7; i++ {
		at := fmt.Sprintf("%d minutes", i+1)
		id := uuid.New()
		exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
		      VALUES ($1, $2, $3, 'Paged', '', 62, 'draft', NOW(), NOW() - $4::interval)`, id, f.user, f.org, at)
		campaigns[id] = true
		id = uuid.New()
		exec(`INSERT INTO api_keys (id, user_id, organization_id, name, key_prefix, key_hash, created_at)
		      VALUES ($1, $2, $3, 'Paged', 'wmbly_pg', $4, NOW() - $5::interval)`, id, f.user, f.org, id.String(), at)
		keys[id] = true
		id = uuid.New()
		exec(`INSERT INTO contact_notes (id, contact_id, organization_id, user_id, content, created_at)
		      VALUES ($1, $2, $3, $4, 'Paged', NOW() - $5::interval)`, id, contact, f.org, f.user, at)
		notes[id] = true
		id = uuid.New()
		exec(`INSERT INTO crm_tasks (id, organization_id, created_by, title, created_at)
		      VALUES ($1, $2, $3, 'Paged', NOW() - $4::interval)`, id, f.org, f.user, at)
		tasks[id] = true
	}

	campaignRepo := NewCampaignRepostory(handle)
	apiKeys := NewAPIKeyRepository(handle)
	crm := NewCRMRepository(pool)
	for _, limit := range []int{1, 3, 8, 50} {
		name := func(list string) string { return fmt.Sprintf("%s limit %d", list, limit) }
		drainUUIDPages(t, name("campaigns"), campaigns, func(cursor *uuid.UUID) ([]uuid.UUID, *string, bool) {
			var c *string
			if cursor != nil {
				s := cursor.String()
				c = &s
			}
			res, err := campaignRepo.Search(ctx, f.org.String(), "", c, nil, "", int32(limit), nil)
			if err != nil {
				t.Fatalf("campaign Search: %v", err)
			}
			ids := make([]uuid.UUID, 0, len(res.Data))
			for _, r := range res.Data {
				ids = append(ids, r.ID)
			}
			return ids, res.Pagination.NextCursor, res.Pagination.HasMore
		})
		drainUUIDPages(t, name("api keys"), keys, func(cursor *uuid.UUID) ([]uuid.UUID, *string, bool) {
			res, xerr := apiKeys.List(ctx, f.org, limit, cursor)
			if xerr != nil {
				t.Fatalf("api key List: %v", xerr)
			}
			ids := make([]uuid.UUID, 0, len(res.Data))
			for _, r := range res.Data {
				ids = append(ids, r.ID)
			}
			return ids, res.Pagination.NextCursor, res.Pagination.HasMore
		})
		drainUUIDPages(t, name("notes"), notes, func(cursor *uuid.UUID) ([]uuid.UUID, *string, bool) {
			res, err := crm.ListNotes(ctx, f.org, contact, limit, cursor)
			if err != nil {
				t.Fatalf("ListNotes: %v", err)
			}
			ids := make([]uuid.UUID, 0, len(res.Data))
			for _, r := range res.Data {
				ids = append(ids, r.ID)
			}
			return ids, res.Pagination.NextCursor, res.Pagination.HasMore
		})
		drainUUIDPages(t, name("tasks"), tasks, func(cursor *uuid.UUID) ([]uuid.UUID, *string, bool) {
			res, err := crm.ListCRMTasks(ctx, f.org, nil, nil, nil, nil, limit, cursor)
			if err != nil {
				t.Fatalf("ListCRMTasks: %v", err)
			}
			ids := make([]uuid.UUID, 0, len(res.Data))
			for _, r := range res.Data {
				ids = append(ids, r.ID)
			}
			return ids, res.Pagination.NextCursor, res.Pagination.HasMore
		})
	}
}
