package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
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
