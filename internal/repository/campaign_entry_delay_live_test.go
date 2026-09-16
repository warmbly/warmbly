package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// The campaign entry delay travels through every write path that touches the
// campaigns row. This is worth a live test because those paths are hand-written
// SQL with positional placeholders and four separate scan lists: a column added
// to one and not the others compiles and lints cleanly and only fails at run
// time. Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignEntryDelay -v
func TestLiveCampaignEntryDelaySurvivesEveryWritePath(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()

	owner, org := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Entry', 'Delay', $2, 'x')`, owner, "delay-"+owner.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Entry Delay', $2, $3)`,
		org, "delay-"+org.String()[:8], owner)
	t.Cleanup(func() {
		c := context.Background()
		for _, sql := range []string{
			`DELETE FROM campaigns WHERE organization_id = $1`,
			`DELETE FROM organizations WHERE id = $1`,
		} {
			if _, err := pool.Exec(c, sql, org); err != nil {
				t.Errorf("cleanup %q: %v", sql, err)
			}
		}
		if _, err := pool.Exec(c, `DELETE FROM users WHERE id = $1`, owner); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})

	repo := NewCampaignRepostory(handle)
	twoDays := 2 * 24 * 60

	// Create with a delay.
	created, xerr := repo.Create(ctx, owner.String(), &org, &models.CreateCampaign{
		Name: "Delayed intro", EntryDelayMinutes: &twoDays,
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	if created.EntryDelayMinutes != twoDays {
		t.Fatalf("created delay = %d, want %d", created.EntryDelayMinutes, twoDays)
	}

	// Read back through the other scan path (GetByID scans its own list).
	fetched, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.EntryDelayMinutes != twoDays {
		t.Fatalf("fetched delay = %d, want %d", fetched.EntryDelayMinutes, twoDays)
	}

	// Update to a new value.
	fourHours := 240
	updated, xerr := repo.Update(ctx, org.String(), created.ID.String(),
		&models.UpdateCampaign{EntryDelayMinutes: &fourHours})
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if updated.EntryDelayMinutes != fourHours {
		t.Fatalf("updated delay = %d, want %d", updated.EntryDelayMinutes, fourHours)
	}

	// Out of range is refused rather than left to the column's CHECK.
	tooLong := 91 * 24 * 60
	if _, xerr = repo.Update(ctx, org.String(), created.ID.String(),
		&models.UpdateCampaign{EntryDelayMinutes: &tooLong}); xerr == nil {
		t.Fatal("a delay past the 90-day ceiling must be refused")
	}
	negative := -1
	if _, xerr = repo.Update(ctx, org.String(), created.ID.String(),
		&models.UpdateCampaign{EntryDelayMinutes: &negative}); xerr == nil {
		t.Fatal("a negative delay must be refused")
	}

	// A duplicate is a configuration copy, so it carries the delay.
	dup, derr := repo.Duplicate(ctx, DuplicateCampaignInput{
		SourceID: created.ID, NewID: uuid.New(), UserID: owner, Name: "Delayed intro (copy)",
	})
	if derr != nil {
		t.Fatalf("duplicate: %v", derr)
	}
	if dup.EntryDelayMinutes != fourHours {
		t.Fatalf("duplicated delay = %d, want %d", dup.EntryDelayMinutes, fourHours)
	}

	// The default is off, so every existing campaign keeps sending immediately.
	plain, xerr := repo.Create(ctx, owner.String(), &org, &models.CreateCampaign{Name: "Plain"})
	if xerr != nil {
		t.Fatalf("create plain: %v", xerr)
	}
	if plain.EntryDelayMinutes != 0 {
		t.Fatalf("default delay = %d, want 0", plain.EntryDelayMinutes)
	}
}
