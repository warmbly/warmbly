package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Paging the conversation list visits every conversation exactly once: the
// cursor names the last row a page returned and the next page reads past it.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUniboxSearchPaging -v
func TestLiveUniboxSearchPagingVisitsEveryConversationOnce(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()

	const total = 7
	want := make(map[uuid.UUID]bool, total)
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < total; i++ {
		id := f.message(t, repo, models.FolderInbox)
		if _, err := handle.Pool.Exec(ctx,
			`UPDATE unibox_emails SET internal_date = $2 WHERE id = $1`,
			id, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("date: %v", err)
		}
		want[id] = true
	}

	folder := models.FolderInbox
	seen := make(map[uuid.UUID]int, total)
	cursor := ""
	for page := 0; page < total+1; page++ {
		res, err := repo.Search(ctx, f.org, &models.MailSearchParams{
			Folder: &folder, PageSize: 2, Cursor: cursor,
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		for _, e := range res.Data {
			seen[e.ID]++
		}
		if !res.Pagination.HasMore {
			break
		}
		cursor = *res.Pagination.NextCursor
	}

	for id := range want {
		if seen[id] != 1 {
			t.Errorf("conversation %s listed %d times, want 1", id, seen[id])
		}
	}
	if len(seen) != total {
		t.Errorf("listed %d conversations, want %d", len(seen), total)
	}
}
