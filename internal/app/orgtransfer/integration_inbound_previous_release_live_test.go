package orgtransfer

import (
	"bytes"
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// exportAsPreviousRelease runs fn with integration_connections registered as the previous release registers it, which knows no sealed inbound column.
func exportAsPreviousRelease(t *testing.T, fn func()) {
	t.Helper()
	table := TableByName["integration_connections"]
	saved := table.Secrets
	table.Secrets = slices.DeleteFunc(slices.Clone(saved), func(sc SecretColumn) bool { return sc.Plaintext != "" })
	defer func() { table.Secrets = saved }()
	fn()
}

// An archive the previous release wrote after the migration keeps its inbound URLs working, sealed here by the sweep.
func TestLivePreviousReleaseArchiveKeepsInboundURLs(t *testing.T) {
	for _, variant := range []string{"unsealed", "foreign ciphertext", "neither carried"} {
		t.Run(variant, func(t *testing.T) {
			keys := perOrgKeys{}
			svc, pool := keyedImportService(t, keys)
			ctx := context.Background()
			src := newTenantFixture(t, pool)
			keys[src.org] = staticKey(1)
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM integration_connections WHERE organization_id = $1`, src.org)
			})

			secret := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0x55}, 24))
			var clear, sealed, digest any = secret, nil, nil
			switch variant {
			case "foreign ciphertext":
				sealed, digest = sealFixture(t, keys[src.org], secret), repository.InboundSecretDigest(secret)
			case "neither carried":
				clear, sealed, digest = nil, sealFixture(t, keys[src.org], secret), repository.InboundSecretDigest(secret)
			}
			conn := uuid.New()
			if _, err := pool.Exec(ctx, `
				INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, inbound_secret, inbound_secret_encrypted, inbound_secret_digest, health)
				VALUES ($1, $2, 'cal_com', 'Bookings', 'connected', 'webhook', $3, $4, $5, 'healthy')`,
				conn, src.org, clear, sealed, digest); err != nil {
				t.Fatal(err)
			}

			passphrase := "previous release archive passphrase"
			groups := []models.OrgDataGroup{models.OrgDataGroupAutomations}
			var archive bytes.Buffer
			exportAsPreviousRelease(t, func() {
				if _, err := svc.ExportTo(ctx, src.org, ExportOptions{Passphrase: passphrase, Groups: groups}, &archive, nil); err != nil {
					t.Fatalf("export: %v", err)
				}
			})
			rows := archiveRows(t, archive.Bytes(), "integration_connections")
			if len(rows) != 1 {
				t.Fatalf("archive holds %d connections, want 1", len(rows))
			}
			for _, c := range []string{"inbound_secret", "inbound_secret_encrypted", "inbound_secret_digest"} {
				if _, ok := rows[0][c]; !ok {
					t.Fatalf("fixture archive lacks %s, so it is not shaped like the previous release's", c)
				}
			}

			if _, err := pool.Exec(ctx, `DELETE FROM integration_connections WHERE organization_id = $1`, src.org); err != nil {
				t.Fatal(err)
			}
			keys[src.org] = staticKey(2)
			result, err := svc.ImportFrom(ctx, src.org, bytes.NewReader(archive.Bytes()), ImportOptions{
				Passphrase: passphrase, Groups: groups, Conflict: models.OrgImportConflictSkip, ActorUserID: src.owner,
			}, nil)
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			var named []string
			for _, w := range result.Warnings {
				if strings.Contains(w, "cal_com (Bookings)") {
					named = append(named, w)
				}
			}

			if variant == "neither carried" {
				assertInboundClosed(t, pool, conn, secret)
				if len(named) != 1 || !strings.Contains(named[0], "without its inbound URL") {
					t.Fatalf("want one warning naming the connection's inbound URL, got %d", len(named))
				}
				return
			}

			if len(named) != 0 {
				t.Fatalf("a connection whose inbound URL travelled was named for reconnection: %v", named)
			}
			var status string
			var plainAt, sealedAt, digestAt *string
			read := func() {
				t.Helper()
				if err := pool.QueryRow(ctx, `
					SELECT status, inbound_secret, inbound_secret_encrypted, inbound_secret_digest
					FROM integration_connections WHERE id = $1`, conn).Scan(&status, &plainAt, &sealedAt, &digestAt); err != nil {
					t.Fatalf("read imported connection: %v", err)
				}
			}
			read()
			if status != string(models.IntegrationStatusConnected) {
				t.Fatalf("carried connection arrived %s", status)
			}
			if plainAt == nil || *plainAt != secret {
				t.Error("the clear inbound secret the archive carried was not kept")
			}
			if sealedAt != nil || digestAt != nil {
				t.Errorf("import kept a sealed value or digest it did not open: sealed=%t digest=%t", sealedAt != nil, digestAt != nil)
			}
			integrations := repository.NewIntegrationRepository(pool)
			if found, err := integrations.GetConnectionByInboundSecret(ctx, models.IntegrationCalCom, secret); err != nil || found == nil || found.ID != conn {
				t.Errorf("imported inbound URL does not resolve: %v", err)
			}

			sweep, ok := integration.NewService(integrations, keys, nil).(interface {
				SealInboundSecrets(context.Context) (int, error)
			})
			if !ok {
				t.Fatal("integration service has no sealing sweep")
			}
			if _, err := sweep.SealInboundSecrets(ctx); err != nil {
				t.Fatal(err)
			}
			read()
			if sealedAt == nil || digestAt == nil || *digestAt != repository.InboundSecretDigest(secret) {
				t.Fatal("the sweep did not seal the imported inbound secret")
			}
			if opened := openFixture(t, staticKey(2), *sealedAt); opened != secret {
				t.Fatal("sealed inbound secret does not open under the destination key")
			}
			if c, err := staticKey(1).Cipher(ctx, src.org); err != nil {
				t.Fatal(err)
			} else if _, err := c.Decrypt(ctx, *sealedAt); err == nil {
				t.Fatal("sealed inbound secret still opens under the source key")
			}
			if found, err := integrations.GetConnectionByInboundSecret(ctx, models.IntegrationCalCom, secret); err != nil || found == nil || found.ID != conn {
				t.Fatalf("sealed inbound URL does not resolve: %v", err)
			}
		})
	}
}
