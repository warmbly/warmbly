package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

func visibilityMailbox(t *testing.T, pool *pgxpool.Pool, f *uniboxFolderFixture, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, created_at)
		VALUES ($1, $2, $3, $4, 'Visibility', '', '', 'smtp_imap', $5)
	`, id, f.user, f.org, id.String()+"@test.local", at)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id = $1`, id)
	})
	return id
}

func visibilityCategory(t *testing.T, handle GroupRepository, f *uniboxFolderFixture, title string) uuid.UUID {
	t.Helper()
	g, xerr := handle.Create(context.Background(), f.org, f.user, &models.GroupCreate{Title: title})
	if xerr != nil {
		t.Fatal(xerr)
	}
	return g.ID
}

func TestLiveUniboxVisibilityCategoriesFollowMailboxMessages(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	foreign := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	groups := NewGroupRepostory(handle, models.Categories)
	ctx := context.Background()
	now := time.Now().UTC()
	hiddenBox := visibilityMailbox(t, handle.Pool, f, now)
	hidden := *f
	hidden.mailbox = hiddenBox

	visibleCat := visibilityCategory(t, groups, f, "Visible")
	hiddenCat := visibilityCategory(t, groups, f, "Hidden")
	filedCat := visibilityCategory(t, groups, f, "Filed")
	unusedCat := visibilityCategory(t, groups, f, "Unused")
	foreignCat := visibilityCategory(t, groups, foreign, "Foreign")
	crossOrgOnlyCat := visibilityCategory(t, groups, f, "Foreign message only")
	f.scopedMessage(t, repo, "visible", "them@example.com", models.FolderInbox, now)
	hidden.scopedMessage(t, repo, "visible", "them@example.com", models.FolderInbox, now)
	hidden.scopedMessage(t, repo, "hidden", "them@example.com", models.FolderInbox, now)
	f.scopedMessage(t, repo, "filed", "them@example.com", models.FolderArchive, now)
	foreign.scopedMessage(t, repo, "foreign", "them@example.com", models.FolderInbox, now)
	for _, label := range []struct {
		org, user uuid.UUID
		thread    string
		category  uuid.UUID
	}{
		{f.org, f.user, "visible", visibleCat},
		{f.org, f.user, "hidden", hiddenCat},
		{f.org, f.user, "filed", filedCat},
		{foreign.org, foreign.user, "visible", foreignCat},
		{foreign.org, foreign.user, "foreign", foreignCat},
		{f.org, f.user, "foreign", crossOrgOnlyCat},
	} {
		if _, err := repo.SetThreadLabels(ctx, label.org, label.user, label.thread, []uuid.UUID{label.category}); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name    string
		allowed []uuid.UUID
		want    int
	}{
		{"visible and cross-org grant", []uuid.UUID{f.mailbox, foreign.mailbox}, 2},
		{"visible", []uuid.UUID{f.mailbox}, 2},
		{"nil grants", nil, 0},
		{"empty grants", []uuid.UUID{}, 0},
		{"none-match grants", models.NoneMatch(), 0},
		{"foreign grants", []uuid.UUID{foreign.mailbox}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cats, err := repo.CategoriesForMailboxes(ctx, f.org, tc.allowed)
			if err != nil || len(cats) != tc.want {
				t.Fatalf("categories = %+v, %v; want %d", cats, err, tc.want)
			}
			for _, c := range cats {
				if c.ID != visibleCat && c.ID != filedCat {
					t.Fatalf("inaccessible category: %+v", c)
				}
				if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
					t.Fatalf("incomplete registry entry: %+v", c)
				}
			}
			overview, err := repo.OverviewForMailboxes(ctx, f.org, tc.allowed)
			if err != nil || len(overview.Categories) != tc.want {
				t.Fatalf("overview = %+v, %v; want %d categories", overview, err, tc.want)
			}
			for _, c := range overview.Categories {
				if c.ID != visibleCat && c.ID != filedCat {
					t.Fatalf("inaccessible overview category: %+v", c)
				}
				wantCount := int64(1)
				if c.ID == filedCat {
					wantCount = 0
				}
				if c.Total != wantCount || c.Unread != wantCount {
					t.Fatalf("overview counts = %+v; want %d", c, wantCount)
				}
			}
		})
	}

	overview, err := repo.Overview(ctx, f.org)
	if err != nil || len(overview.Categories) != 5 {
		t.Fatalf("unrestricted palette changed: %+v, %v", overview, err)
	}
	palette, xerr := groups.List(ctx, f.org)
	if xerr != nil || len(palette) != 5 {
		t.Fatalf("unrestricted registry changed: %+v, %v", palette, xerr)
	}
	unusedFound := false
	for _, c := range palette {
		unusedFound = unusedFound || c.ID == unusedCat
	}
	if !unusedFound {
		t.Fatal("unrestricted registry omitted an unused category")
	}
}

func TestLiveUniboxVisibilityLabelsHideUnknownAndInaccessibleThreads(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	foreign := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	now := time.Now().UTC()
	hidden := *f
	hidden.mailbox = visibilityMailbox(t, handle.Pool, f, now)
	cat := visibilityCategory(t, NewGroupRepostory(handle, models.Categories), f, "Visible label")
	f.scopedMessage(t, repo, "mixed", "them@example.com", models.FolderInbox, now)
	hidden.scopedMessage(t, repo, "mixed", "them@example.com", models.FolderInbox, now)
	hidden.scopedMessage(t, repo, "hidden", "them@example.com", models.FolderInbox, now)
	f.scopedMessage(t, repo, "unlabelled", "them@example.com", models.FolderInbox, now)
	foreign.scopedMessage(t, repo, "foreign", "them@example.com", models.FolderInbox, now)
	for _, thread := range []string{"mixed", "hidden", "foreign"} {
		if _, err := repo.SetThreadLabels(ctx, f.org, f.user, thread, []uuid.UUID{cat}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, thread string
		org          uuid.UUID
		allowed      []uuid.UUID
		want         int
		notFound     bool
	}{
		{"mixed thread", "mixed", f.org, []uuid.UUID{f.mailbox}, 1, false},
		{"mixed grants", "mixed", f.org, []uuid.UUID{f.mailbox, foreign.mailbox}, 1, false},
		{"unlabelled", "unlabelled", f.org, []uuid.UUID{f.mailbox}, 0, false},
		{"unrestricted", "hidden", f.org, nil, 1, false},
		{"hidden", "hidden", f.org, []uuid.UUID{f.mailbox}, 0, true},
		{"unknown", "unknown", f.org, []uuid.UUID{f.mailbox}, 0, true},
		{"unrestricted unknown", "unknown", f.org, nil, 0, true},
		{"empty grants", "mixed", f.org, []uuid.UUID{}, 0, true},
		{"none-match grants", "mixed", f.org, models.NoneMatch(), 0, true},
		{"foreign mailbox", "foreign", f.org, []uuid.UUID{foreign.mailbox}, 0, true},
		{"foreign organization", "mixed", foreign.org, []uuid.UUID{f.mailbox}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			labels, err := repo.ListThreadLabelsWithin(ctx, tc.org, tc.thread, tc.allowed)
			if tc.notFound {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("labels = %+v, %v; want identical not-found", labels, err)
				}
				return
			}
			if err != nil || len(labels) != tc.want {
				t.Fatalf("labels = %+v, %v; want %d", labels, err, tc.want)
			}
		})
	}
}

func TestLiveEmailCursorVisibilityRequiresOrganizationAndMailboxGrants(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	foreign := newUniboxFolderFixture(t, handle.Pool)
	ctx := context.Background()
	now := time.Now().UTC()
	newer := visibilityMailbox(t, handle.Pool, f, now.Add(3*time.Hour))
	older := visibilityMailbox(t, handle.Pool, f, now.Add(time.Hour))
	hidden := visibilityMailbox(t, handle.Pool, f, now.Add(2*time.Hour))
	if _, err := handle.Pool.Exec(ctx, `UPDATE email_accounts SET created_at = $2 WHERE id = $1`, foreign.mailbox, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	repo := NewEmailRepostory(handle, nil)
	allowed := []uuid.UUID{newer, older}
	cursor := paging.UUIDString
	for _, tc := range []struct {
		name    string
		cursor  *string
		allowed []uuid.UUID
		want    int
	}{
		{"initial restricted page", nil, allowed, 2},
		{"allowed cursor", cursor(newer), allowed, 1},
		{"hidden cursor", cursor(hidden), allowed, 0},
		{"foreign cursor", cursor(foreign.mailbox), allowed, 0},
		{"foreign cursor unrestricted", cursor(foreign.mailbox), nil, 0},
		{"unknown cursor", cursor(uuid.New()), allowed, 0},
		{"empty grants", nil, []uuid.UUID{}, 0},
		{"empty grants cursor", cursor(newer), []uuid.UUID{}, 0},
		{"none-match grants", nil, models.NoneMatch(), 0},
		{"mixed cross-org grants", nil, []uuid.UUID{newer, foreign.mailbox}, 1},
		{"unrestricted page", nil, nil, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, xerr := repo.Search(ctx, f.org.String(), "", tc.cursor, nil, 50, tc.allowed)
			if xerr != nil || len(page.Data) != tc.want {
				t.Fatalf("page = %+v, %v; want %d", page, xerr, tc.want)
			}
			if tc.name == "allowed cursor" && page.Data[0].ID != older {
				t.Fatalf("allowed cursor returned %+v; want older mailbox %s", page.Data, older)
			}
		})
	}
}
