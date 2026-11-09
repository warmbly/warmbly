package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// Issue #267: forms CRUD, the category aggregation scan and the submission
// counter transaction, proven against the real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveForm -v

func TestLiveFormLifecycle(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewFormRepository(handle)
	ctx := context.Background()

	category := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO categories (id, organization_id, user_id, title, color, position) VALUES ($1, $2, $3, 'Form leads', '#00ff00', 0)`, category, f.org, f.owner); err != nil {
		t.Fatalf("fixture category: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		if _, err := pool.Exec(c, `DELETE FROM forms WHERE organization_id = $1`, f.org); err != nil {
			t.Errorf("cleanup forms: %v", err)
		}
		if _, err := pool.Exec(c, `DELETE FROM categories WHERE id = $1`, category); err != nil {
			t.Errorf("cleanup category: %v", err)
		}
	})

	created, xerr := repo.Create(ctx, f.org, &f.owner, &models.Form{
		PublicID:       "livetest-" + uuid.New().String()[:13],
		Name:           "Live test form",
		Status:         models.FormStatusDraft,
		Fields:         []models.FormField{{ID: "email", Type: models.FormFieldEmail, Label: "Email", Required: true, MapTo: "email"}},
		SuccessMessage: "Thanks",
		CategoryIDs:    []uuid.UUID{category},
		AllowedDomains: []string{"example.com"},
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	if len(created.CategoryIDs) != 1 || created.CategoryIDs[0] != category {
		t.Fatalf("category aggregation scan: %+v", created.CategoryIDs)
	}
	if len(created.AllowedDomains) != 1 || created.AllowedDomains[0] != "example.com" {
		t.Fatalf("allowed domains round-trip: %+v", created.AllowedDomains)
	}

	// Publish through Update; the campaign link is org-scoped.
	created.Status = models.FormStatusPublished
	created.CampaignID = &f.campaign
	updated, xerr := repo.Update(ctx, f.org, created)
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if updated.Status != models.FormStatusPublished || updated.CampaignID == nil {
		t.Fatalf("update round-trip: %+v", updated)
	}

	byPublic, xerr := repo.GetByPublicID(ctx, created.PublicID)
	if xerr != nil {
		t.Fatalf("get by public id: %v", xerr)
	}
	if byPublic.ID != created.ID {
		t.Fatal("public id resolved the wrong form")
	}

	if xerr := repo.RecordView(ctx, created.ID); xerr != nil {
		t.Fatalf("record view: %v", xerr)
	}
	sub, xerr := repo.CreateSubmission(ctx, &models.FormSubmission{
		FormID:         created.ID,
		OrganizationID: f.org,
		ContactID:      &f.contact,
		Data:           map[string]any{"email": "live@test.dev", "topics": []string{"a", "b"}},
		SourceURL:      "https://example.com/pricing",
	})
	if xerr != nil {
		t.Fatalf("create submission: %v", xerr)
	}
	if xerr := repo.LogSubmissionActivity(ctx, f.org, f.contact, created.ID, created.Name, sub.ID); xerr != nil {
		t.Fatalf("log activity: %v", xerr)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM contact_activities WHERE contact_id = $1 AND activity_type = 'form_submitted'`, f.contact); err != nil {
			t.Errorf("cleanup activities: %v", err)
		}
	})

	after, xerr := repo.Get(ctx, f.org, created.ID)
	if xerr != nil {
		t.Fatalf("get after submit: %v", xerr)
	}
	if after.ViewsCount != 1 || after.SubmissionsCount != 1 || after.LastSubmissionAt == nil {
		t.Fatalf("counters: views=%d subs=%d last=%v", after.ViewsCount, after.SubmissionsCount, after.LastSubmissionAt)
	}

	subs, hasMore, xerr := repo.ListSubmissions(ctx, f.org, created.ID, 10, nil)
	if xerr != nil {
		t.Fatalf("list submissions: %v", xerr)
	}
	if len(subs) != 1 || hasMore {
		t.Fatalf("list: %d hasMore=%v", len(subs), hasMore)
	}
	if subs[0].ContactEmail == "" {
		t.Fatal("contact join missing")
	}
	if got, ok := subs[0].Data["topics"].([]any); !ok || len(got) != 2 {
		t.Fatalf("data round-trip: %#v", subs[0].Data["topics"])
	}

	if xerr := repo.DeleteSubmission(ctx, f.org, created.ID, subs[0].ID); xerr != nil {
		t.Fatalf("delete submission: %v", xerr)
	}
	after, xerr = repo.Get(ctx, f.org, created.ID)
	if xerr != nil {
		t.Fatalf("get after delete: %v", xerr)
	}
	if after.SubmissionsCount != 0 {
		t.Fatalf("counter after delete: %d", after.SubmissionsCount)
	}

	// Tenancy: another org must see nothing.
	if _, xerr := repo.Get(ctx, f.other, created.ID); xerr == nil {
		t.Fatal("cross-org read succeeded")
	}
	if xerr := repo.Delete(ctx, f.org, created.ID); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}
}

// Issue #343: the builder's "New form" hands the repository a Form with no
// allowed domains and no fields at all. Both columns reject NULL, so the nil
// slices have to reach Postgres as empty values instead.
func TestLiveFormCreateEmptyLists(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewFormRepository(handle)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM forms WHERE organization_id = $1`, f.org); err != nil {
			t.Errorf("cleanup forms: %v", err)
		}
	})

	created, xerr := repo.Create(ctx, f.org, &f.owner, &models.Form{
		PublicID:       "livetest-" + uuid.New().String()[:13],
		Name:           "Empty lists",
		Status:         models.FormStatusDraft,
		SuccessMessage: "Thanks",
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	if created.AllowedDomains == nil || len(created.AllowedDomains) != 0 {
		t.Fatalf("allowed domains: %#v", created.AllowedDomains)
	}
	if created.Fields == nil || len(created.Fields) != 0 {
		t.Fatalf("fields: %#v", created.Fields)
	}

	// The same nil slices on the way back out (publishing a form the builder
	// never gave domains to).
	created.AllowedDomains = nil
	created.Fields = nil
	created.Status = models.FormStatusPublished
	updated, xerr := repo.Update(ctx, f.org, created)
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if len(updated.AllowedDomains) != 0 || len(updated.Fields) != 0 {
		t.Fatalf("update round-trip: %#v %#v", updated.AllowedDomains, updated.Fields)
	}
}

// Issue #1073: paging through submissions that share a timestamp, or sit a
// microsecond apart, at every page boundary returns each one exactly once.
func TestLiveFormSubmissionsPageAcrossEqualAndCloseTimestamps(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewFormRepository(handle)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM forms WHERE organization_id = $1`, f.org); err != nil {
			t.Errorf("cleanup forms: %v", err)
		}
	})

	form, xerr := repo.Create(ctx, f.org, &f.owner, &models.Form{
		PublicID:       "livetest-" + uuid.New().String()[:13],
		Name:           "Paging",
		Status:         models.FormStatusPublished,
		SuccessMessage: "Thanks",
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}

	// Five rows on one microsecond, two a microsecond either side, one a
	// millisecond away: every page size below lands a boundary inside a tie.
	base := time.Date(2026, 3, 1, 12, 0, 0, 123456000, time.UTC)
	offsets := []time.Duration{time.Millisecond, time.Microsecond, 0, 0, 0, 0, 0, -time.Microsecond}
	for _, off := range offsets {
		if _, err := pool.Exec(ctx, `INSERT INTO form_submissions (form_id, organization_id, created_at) VALUES ($1, $2, $3)`,
			form.ID, f.org, base.Add(off)); err != nil {
			t.Fatalf("insert submission: %v", err)
		}
	}
	var want []uuid.UUID
	rows, err := pool.Query(ctx, `SELECT id FROM form_submissions WHERE form_id = $1 ORDER BY created_at DESC, id DESC`, form.ID)
	if err != nil {
		t.Fatalf("expected order: %v", err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		want = append(want, id)
	}
	rows.Close()

	for limit := 1; limit <= len(want); limit++ {
		var got []uuid.UUID
		var after *models.FormSubmissionPosition
		for pages := 0; ; pages++ {
			if pages > len(want) {
				t.Fatalf("limit %d: paging did not terminate", limit)
			}
			page, hasMore, xerr := repo.ListSubmissions(ctx, f.org, form.ID, limit, after)
			if xerr != nil {
				t.Fatalf("limit %d: list: %v", limit, xerr)
			}
			for _, s := range page {
				got = append(got, s.ID)
			}
			if !hasMore {
				break
			}
			// Round-trip the opaque cursor the handler hands out.
			last := page[len(page)-1]
			at, id, xerr := paging.DecodeTimeCursor(*paging.EncodeTime(last.CreatedAt, last.ID))
			if xerr != nil {
				t.Fatalf("limit %d: cursor: %v", limit, xerr)
			}
			after = &models.FormSubmissionPosition{At: at, ID: id}
		}
		if len(got) != len(want) {
			t.Fatalf("limit %d: got %d rows, want %d", limit, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("limit %d: row %d is %s, want %s", limit, i, got[i], want[i])
			}
		}
	}

	// A timestamp-only cursor from the previous release keeps microseconds:
	// strictly before the tie returns only the row a microsecond older.
	legacy := base.Format(time.RFC3339Nano)
	at, err := time.Parse(time.RFC3339Nano, legacy)
	if err != nil {
		t.Fatalf("parse legacy cursor: %v", err)
	}
	older, _, xerr := repo.ListSubmissions(ctx, f.org, form.ID, 50, &models.FormSubmissionPosition{At: at})
	if xerr != nil {
		t.Fatalf("legacy list: %v", xerr)
	}
	if len(older) != 1 || older[0].ID != want[len(want)-1] {
		t.Fatalf("legacy before %s returned %d rows", legacy, len(older))
	}
}
