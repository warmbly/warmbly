package contact

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A member adding an address a teammate already holds updates the workspace's contact instead of creating a second one.
func TestLiveAddByTeammateUpdatesTheWorkspaceContact(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	mate := uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Team', 'Mate', $2, 'x')`,
		mate, "mate-"+mate.String()[:8]+"@test.local")
	t.Cleanup(func() {
		c := context.Background()
		_, _ = f.pool.Exec(c, `DELETE FROM contacts WHERE user_id = $1`, mate)
		_, _ = f.pool.Exec(c, `DELETE FROM organization_members WHERE user_id = $1`, mate)
		_, _ = f.pool.Exec(c, `DELETE FROM users WHERE id = $1`, mate)
	})
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at) VALUES ($1, $2, 'member', NOW())`, f.org, mate)

	first, xerr := f.svc.Add(ctx, f.user.String(), f.org, []models.AddContact{{Email: "shared@lead.test", FirstName: "Dana"}})
	if xerr != nil {
		t.Fatalf("owner add: %v", xerr.Message)
	}
	again, xerr := f.svc.Add(ctx, mate.String(), f.org, []models.AddContact{{Email: "Shared@Lead.test", Company: "Acme"}, {Email: "fresh@lead.test"}})
	if xerr != nil {
		t.Fatalf("teammate add: %v", xerr.Message)
	}
	if again[0].ID != first[0].ID || again[0].IsNew {
		t.Fatalf("teammate's add made contact %s (new=%v), want the existing %s", again[0].ID, again[0].IsNew, first[0].ID)
	}
	if again[0].FirstName != "Dana" || again[0].Company != "Acme" {
		t.Errorf("merged contact = %q at %q, want Dana at Acme", again[0].FirstName, again[0].Company)
	}
	if !again[1].IsNew {
		t.Error("an address nobody held was not created")
	}
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM contacts WHERE organization_id = $1 AND LOWER(email) = 'shared@lead.test'`, f.org).Scan(&n); err != nil || n != 1 {
		t.Fatalf("workspace holds %d copies of the address (err %v), want 1", n, err)
	}
}
