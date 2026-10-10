package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestLiveCampaignReplyRepairDoesNotSkipMatchingMessagesAcrossRawPages(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	contactID := uuid.New()
	if _, err := handle.Pool.Exec(ctx, `INSERT INTO contacts (id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields)
		VALUES ($1, $2, $3, '', '', 'eligible@test.local', '', '', '{}'::jsonb)`, contactID, f.user, f.org); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = handle.Pool.Exec(context.Background(), `DELETE FROM contacts WHERE id=$1`, contactID) })

	prefix := uuid.New().String()[:14] + "4000-8000-"
	id := func(i int) uuid.UUID { return uuid.MustParse(prefix + fmt.Sprintf("%012x", i)) }
	var values [][]any
	for i := 1; i <= 502; i++ {
		folder := "inbox"
		from := "not-a-contact@test.local"
		if i == 501 || i == 502 {
			from = "Name (eligible@test.local)"
		}
		if i == 502 {
			folder = "sent"
		}
		values = append(values, []any{id(i), f.user, f.mailbox, time.Now().UTC(), folder, folder, []string{from}})
	}
	if _, err := handle.Pool.CopyFrom(ctx, pgx.Identifier{"unibox_emails"},
		[]string{"id", "user_id", "email_id", "created_at", "folder", "provider_folder", "from_addr"}, pgx.CopyFromRows(values)); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Hour)
	first, err := repo.ListUnprocessedCampaignReplies(ctx, since, id(0), 100)
	if err != nil || first.Done || first.NextCursor != id(500) || len(first.Events) != 0 {
		t.Fatalf("first page: next=%v done=%t matches=%d err=%v", first.NextCursor, first.Done, len(first.Events), err)
	}
	second, err := repo.ListUnprocessedCampaignReplies(ctx, since, first.NextCursor, 100)
	if err != nil || !second.Done || second.NextCursor != id(501) || len(second.Events) != 1 || second.Events[0].Message.ID != id(501) {
		t.Fatalf("second page: next=%v done=%t matches=%d err=%v", second.NextCursor, second.Done, len(second.Events), err)
	}

	if _, err := handle.Pool.Exec(ctx, `UPDATE unibox_emails SET from_addr=ARRAY['eligible@test.local']
		WHERE email_id=$1 AND id >= $2 AND id <= $3`, f.mailbox, id(1), id(110)); err != nil {
		t.Fatal(err)
	}
	dense, err := repo.ListUnprocessedCampaignReplies(ctx, since, id(0), 100)
	if err != nil || dense.Done || dense.NextCursor != id(100) || len(dense.Events) != 100 {
		t.Fatalf("full result page: next=%v done=%t matches=%d err=%v", dense.NextCursor, dense.Done, len(dense.Events), err)
	}
	remainder, err := repo.ListUnprocessedCampaignReplies(ctx, since, dense.NextCursor, 100)
	if err != nil || !remainder.Done || remainder.NextCursor != id(501) || len(remainder.Events) != 11 {
		t.Fatalf("remaining page: next=%v done=%t matches=%d err=%v", remainder.NextCursor, remainder.Done, len(remainder.Events), err)
	}
}
