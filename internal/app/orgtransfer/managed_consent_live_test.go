package orgtransfer

import (
	"context"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"testing"
	"time"
)

func TestLiveImportManagedConsentKeepsRestrictionsButCannotResumeSourceHandles(t *testing.T) {
	svc, pool := liveImportService(t)
	dest := newTenantFixture(t, pool)
	ctx := context.Background()
	for _, table := range []string{"cloud_managed_consents", "pool_link_managed_operations"} {
		for _, state := range []string{"revoked", "pending"} {
			id := uuid.New()
			row := map[string]any{"id": id, "organization_id": uuid.New(), "instance_id": uuid.New(), "remote_id": uuid.New(),
				"planned_account_id": uuid.New(), "session_hash": "opaque-source-handle-hash", "kind": "oauth", "provider": "gmail", "consent_state": state, "expires_at": time.Now().Add(time.Minute)}
			if table == "cloud_managed_consents" {
				row["user_id"] = dest.owner
				row["cloud_account_id"] = uuid.New()
			}
			archive := tenantArchive(t, map[string][]map[string]any{table: {row}})
			if _, err := svc.ImportFrom(ctx, dest.org, archive, ImportOptions{Conflict: models.OrgImportConflictOverwrite, ActorUserID: dest.owner}, nil); err != nil {
				t.Fatal(err)
			}
			var got string
			var cleared bool
			query := `SELECT consent_state, instance_id IS NULL AND remote_id IS NULL AND session_hash IS NULL AND planned_account_id IS NULL FROM ` + table + ` WHERE id=$1 AND organization_id=$2`
			if err := pool.QueryRow(ctx, query, id, dest.org).Scan(&got, &cleared); err != nil {
				t.Fatal(err)
			}
			if got != state || !cleared {
				t.Fatalf("%s: state=%s, handles cleared=%v", table, got, cleared)
			}
		}
	}
}
