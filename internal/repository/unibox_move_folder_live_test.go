package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// Filing a conversation in the thread header moves it here and not at the
// provider, which only works because unibox_emails keeps the two placements in
// separate columns (migration 000146). models.ResolveFolderSync covers the
// decision; these run the statements it feeds against a real schema, because
// the interesting parts are things Go cannot check: that the insert seeds both
// columns from one bound value, that the full-row scan still lines up after a
// column was appended, and that the move leaves provider_folder alone.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUniboxMoveFolder -v

type uniboxFolderFixture struct {
	org     uuid.UUID
	user    uuid.UUID
	mailbox uuid.UUID
}

func newUniboxFolderFixture(t *testing.T, pool *pgxpool.Pool) *uniboxFolderFixture {
	t.Helper()
	ctx := context.Background()
	f := &uniboxFolderFixture{org: uuid.New(), user: uuid.New(), mailbox: uuid.New()}
	tag := "pr435-" + f.org.String()[:8]

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Folder', 'Live', $2, 'x')`, f.user, tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'PR 435', $2, $3)`, f.org, tag, f.user)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
	      VALUES ($1, $2, 'owner', NOW())`, f.org, f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Folder', '', '', 'smtp_imap')`, f.mailbox, f.user, f.org, tag+"-mb@test.local")

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM unibox_emails WHERE email_id = $1`, f.mailbox)
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, f.mailbox)
		_, _ = pool.Exec(c, `DELETE FROM organization_members WHERE organization_id = $1`, f.org)
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, f.org)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = $1`, f.user)
	})
	return f
}

func liveUniboxFolderDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	return handle
}

func (f *uniboxFolderFixture) message(t *testing.T, repo UniboxRepository, folder string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	err := repo.CreateEntry(context.Background(), f.user, &models.EmailMessageStoreData{
		ID: id, EmailID: f.mailbox, Folder: folder,
		ThreadID: "thread-" + id.String(), MessageID: "<" + id.String() + "@test.local>",
		FromAddr: []string{"Centous Support (support@centous.com)"},
		ToAddr:   []string{"me@test.local"},
		Subject:  "Filing", Snippet: "Filing",
		InternalDate: now, SentDate: now, CreatedAt: now, UpdatedAt: now,
		Seen: true,
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	return id
}

// A new message is in one place, so both columns start there. Getting this
// wrong would make every row read as locally filed from the moment it arrives.
func TestLiveUniboxMoveFolderSeedsBothColumnsOnInsert(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)

	id := f.message(t, repo, models.FolderInbox)
	got, err := repo.GetByID(context.Background(), f.user, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Folder != models.FolderInbox || got.ProviderFolder != models.FolderInbox {
		t.Fatalf("folder=%q provider_folder=%q, want both %q", got.Folder, got.ProviderFolder, models.FolderInbox)
	}
}

// The move writes folder and nothing else. provider_folder staying put is what
// lets the next sync tell this apart from the provider moving the message.
func TestLiveUniboxMoveFolderLeavesTheProviderPlacementAlone(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()

	id := f.message(t, repo, models.FolderInbox)
	if err := repo.MoveToFolderBulk(ctx, f.org, []uuid.UUID{id}, models.FolderTrash); err != nil {
		t.Fatalf("MoveToFolderBulk: %v", err)
	}

	got, err := repo.GetByID(ctx, f.user, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Folder != models.FolderTrash {
		t.Fatalf("folder = %q, want %q", got.Folder, models.FolderTrash)
	}
	if got.ProviderFolder != models.FolderInbox {
		t.Fatalf("provider_folder = %q, want it untouched at %q", got.ProviderFolder, models.FolderInbox)
	}

	// And the message really has left every default view.
	res, err := repo.Search(ctx, f.org, &models.MailSearchParams{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, row := range res.Data {
		if row.ID == id {
			t.Fatal("a trashed message is still in the unscoped list")
		}
	}
}

// Another organization's ids are not this organization's to file.
func TestLiveUniboxMoveFolderIsOrgScoped(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	mine := newUniboxFolderFixture(t, handle.Pool)
	theirs := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()

	id := theirs.message(t, repo, models.FolderInbox)
	if err := repo.MoveToFolderBulk(ctx, mine.org, []uuid.UUID{id}, models.FolderTrash); err != nil {
		t.Fatalf("MoveToFolderBulk: %v", err)
	}

	got, err := repo.GetByID(ctx, theirs.user, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Folder != models.FolderInbox {
		t.Fatalf("folder = %q, want another org's message left at %q", got.Folder, models.FolderInbox)
	}
}

// The thread lookup behind the "label email" automation step reads the address
// out of the raw header, and the IMAP sync writes those as "Name (addr)".
func TestLiveUniboxLatestThreadIDMatchesTheParenthesisedForm(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()

	id := f.message(t, repo, models.FolderInbox)
	threadID, err := repo.LatestThreadIDForContact(ctx, f.user, "support@centous.com")
	if err != nil {
		t.Fatalf("LatestThreadIDForContact: %v", err)
	}
	if threadID != "thread-"+id.String() {
		t.Fatalf("thread = %q, want %q", threadID, "thread-"+id.String())
	}

	// Still an exact match, never a substring one.
	other, err := repo.LatestThreadIDForContact(ctx, f.user, "upport@centous.com")
	if err != nil {
		t.Fatalf("LatestThreadIDForContact: %v", err)
	}
	if other != "" {
		t.Fatalf("thread = %q, want no match for a partial address", other)
	}
}
