package orgtransfer

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// A grant can only be refreshed by the client that issued it, so an import never leaves an imported app's id
// pointing at a different client, in either conflict mode, and never rewrites the client of an app the
// destination already holds.
func TestLiveImportRefusesAnOAuthAppThatWouldChangeClient(t *testing.T) {
	svc, pool := liveImportService(t)
	ctx := context.Background()

	appRow := func(id, org uuid.UUID, client string) map[string]any {
		return map[string]any{
			"id": id, "organization_id": org, "provider": "gmail", "client_id": client,
			"client_secret": "", "app_name": "Acme Mail", "redirect_url": "",
		}
	}
	cases := []struct {
		name     string
		conflict models.OrgImportConflict
		sameID   bool
		client   string
		refused  bool
	}{
		{"same id, other client, overwrite", models.OrgImportConflictOverwrite, true, "other.apps.googleusercontent.com", true},
		{"same id, other client, skip", models.OrgImportConflictSkip, true, "other.apps.googleusercontent.com", true},
		{"other id, same provider, overwrite", models.OrgImportConflictOverwrite, false, "dest.apps.googleusercontent.com", true},
		{"other id, same provider, skip", models.OrgImportConflictSkip, false, "dest.apps.googleusercontent.com", true},
		{"same id, same client, skip", models.OrgImportConflictSkip, true, "dest.apps.googleusercontent.com", false},
		{"same id, same client, overwrite", models.OrgImportConflictOverwrite, true, "dest.apps.googleusercontent.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dest := newTenantFixture(t, pool)
			existing := uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO organization_oauth_apps (id, organization_id, provider, client_id, client_secret, app_name)
				VALUES ($1, $2, 'gmail', 'dest.apps.googleusercontent.com', 'sealed', 'Dest Mail')`, existing, dest.org); err != nil {
				t.Fatalf("fixture app: %v", err)
			}
			archived := uuid.New()
			if c.sameID {
				archived = existing
			}
			archive := tenantArchive(t, map[string][]map[string]any{
				"organization_oauth_apps": {appRow(archived, uuid.New(), c.client)},
			})
			_, err := svc.ImportFrom(ctx, dest.org, archive, ImportOptions{Conflict: c.conflict, ActorUserID: dest.owner}, nil)
			if c.refused != errors.Is(err, ErrOAuthAppConflict) {
				t.Fatalf("import err = %v, want refused=%v", err, c.refused)
			}
			if !c.refused && err != nil {
				t.Fatalf("import: %v", err)
			}
			var client, secret string
			if err := pool.QueryRow(ctx, `SELECT client_id, client_secret FROM organization_oauth_apps WHERE id = $1`, existing).Scan(&client, &secret); err != nil {
				t.Fatal(err)
			}
			if client != "dest.apps.googleusercontent.com" {
				t.Fatalf("the destination app's client changed to %s", client)
			}
			// The archive carries no secret, which never blanks the one the destination holds.
			if secret != "sealed" {
				t.Fatalf("the destination app's secret changed to %q", secret)
			}
		})
	}
}
