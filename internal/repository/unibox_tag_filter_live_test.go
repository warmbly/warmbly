package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

func liveTag(t *testing.T, pool *pgxpool.Pool, org uuid.UUID, mailboxes ...uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO tags (id, title, color, position, organization_id) VALUES ($1, $2, '#000000', 0, $3)`,
		id, "tag-"+id.String()[:8], org); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tags WHERE id = $1`, id) })
	for _, m := range mailboxes {
		if _, err := pool.Exec(ctx, `INSERT INTO email_tags (email_id, tag_id) VALUES ($1, $2)`, m, id); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func liveMessageIn(t *testing.T, pool *pgxpool.Pool, user, mailbox uuid.UUID) string {
	t.Helper()
	thread := "tagged-" + uuid.NewString()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO unibox_emails (id, user_id, email_id, thread_id, message_id, from_addr, to_addr, subject, internal_date, seen, folder)
		VALUES (gen_random_uuid(), $1, $2, $3, $3, ARRAY['Someone <[EMAIL]>'], ARRAY['[EMAIL]'], 'Tagged', NOW(), false, 'inbox')`,
		user, mailbox, thread); err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestLiveUniboxSearchTagFilter(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	other := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)

	tagged := visibilityMailbox(t, handle.Pool, f, time.Now())
	untagged := visibilityMailbox(t, handle.Pool, f, time.Now())
	inTagged := liveMessageIn(t, handle.Pool, f.user, tagged)
	liveMessageIn(t, handle.Pool, f.user, untagged)
	tag := liveTag(t, handle.Pool, f.org, tagged)
	empty := liveTag(t, handle.Pool, f.org)
	// Another workspace's tag on its own mailbox never widens this one's list.
	foreign := liveTag(t, handle.Pool, other.org, other.mailbox)
	liveMessageIn(t, handle.Pool, other.user, other.mailbox)

	threads := func(p *models.MailSearchParams) []string {
		t.Helper()
		p.PageSize = 50
		res, err := repo.Search(context.Background(), f.org, p)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(res.Data))
		for _, m := range res.Data {
			out = append(out, m.ThreadID)
		}
		return out
	}

	for _, tc := range []struct {
		name   string
		params models.MailSearchParams
		want   []string
	}{
		{"tag", models.MailSearchParams{TagIDs: []uuid.UUID{tag}}, []string{inTagged}},
		{"tag on no mailbox", models.MailSearchParams{TagIDs: []uuid.UUID{empty}}, nil},
		{"another workspace's tag", models.MailSearchParams{TagIDs: []uuid.UUID{foreign}}, nil},
		{"tag and allowlist intersect", models.MailSearchParams{TagIDs: []uuid.UUID{tag}, EmailAccountIDs: []uuid.UUID{untagged}}, nil},
		{"tag within allowlist", models.MailSearchParams{TagIDs: []uuid.UUID{tag}, EmailAccountIDs: []uuid.UUID{tagged, untagged}}, []string{inTagged}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := threads(&tc.params)
			if fmt.Sprint(got) != fmt.Sprint(append([]string{}, tc.want...)) {
				t.Fatalf("threads = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLiveUniboxOverviewListsEveryMailbox(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	for range 240 {
		visibilityMailbox(t, handle.Pool, f, time.Now())
	}
	ov, err := repo.Overview(context.Background(), f.org)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Mailboxes) != 241 {
		t.Fatalf("overview lists %d mailboxes, want 241", len(ov.Mailboxes))
	}
}
