package orgtransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Integration inbound secrets travel in an archive only as passphrase-sealed values.
func TestLiveArchiveCarriesInboundSecretsOnlySealed(t *testing.T) {
	for _, passphrase := range []string{"inbound secret archive passphrase", ""} {
		name := "with passphrase"
		if passphrase == "" {
			name = "without passphrase"
		}
		t.Run(name, func(t *testing.T) {
			_, pool := liveImportService(t)
			ctx := context.Background()
			src := newTenantFixture(t, pool)
			srcKeys := cipher.NewStatic(bytes.Repeat([]byte{1}, 32))
			rekeyed := cipher.NewStatic(bytes.Repeat([]byte{2}, 32))
			keys := perOrgKeys{src.org: srcKeys}
			svc := NewService(repository.NewOrgTransferRepository(&db.DB{Pool: pool}), keys, nil, nil, InstanceInfo{})

			sealedSecret := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0x11}, 24))
			clearSecret := "clay_" + hex.EncodeToString(bytes.Repeat([]byte{0x22}, 24))
			srcCipher, err := srcKeys.Cipher(ctx, src.org)
			if err != nil {
				t.Fatal(err)
			}
			sealedAtRest, err := srcCipher.Encrypt(ctx, sealedSecret)
			if err != nil {
				t.Fatal(err)
			}
			sealedConn, clearConn := uuid.New(), uuid.New()
			// One row sealed as this release writes it, one only in the clear as the previous release does.
			if _, err := pool.Exec(ctx, `
				INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, inbound_secret, inbound_secret_encrypted, inbound_secret_digest, health)
				VALUES ($1, $3, 'cal_com', 'Bookings', 'connected', 'webhook', $4, $5, $6, 'healthy'),
				       ($2, $3, 'clay', 'Enrichment', 'connected', 'webhook', $7, NULL, NULL, 'healthy')`,
				sealedConn, clearConn, src.org, sealedSecret, sealedAtRest, repository.InboundSecretDigest(sealedSecret), clearSecret); err != nil {
				t.Fatal(err)
			}

			var archive bytes.Buffer
			if _, err := svc.ExportTo(ctx, src.org, ExportOptions{Passphrase: passphrase, Groups: []models.OrgDataGroup{models.OrgDataGroupAutomations}}, &archive, nil); err != nil {
				t.Fatalf("export: %v", err)
			}
			rows := archiveConnectionRows(t, archive.Bytes(), sealedSecret, clearSecret)
			if len(rows) != 2 {
				t.Fatalf("archive holds %d connections, want 2", len(rows))
			}
			for _, row := range rows {
				for _, c := range []string{"inbound_secret", "inbound_secret_digest"} {
					if _, ok := row[c]; ok {
						t.Fatalf("archive carries %s", c)
					}
				}
				sealed := jsonString(row["inbound_secret_encrypted"])
				if passphrase != "" && !IsSealed(sealed) {
					t.Fatal("inbound secret is not passphrase-sealed in the archive")
				}
				if passphrase == "" && sealed != "" {
					t.Fatal("an archive without a passphrase carries an inbound secret")
				}
			}

			// The workspace comes back under a different DEK, as it would on another instance.
			if _, err := pool.Exec(ctx, `DELETE FROM integration_connections WHERE organization_id = $1`, src.org); err != nil {
				t.Fatal(err)
			}
			keys[src.org] = rekeyed
			result, err := svc.ImportFrom(ctx, src.org, bytes.NewReader(archive.Bytes()), ImportOptions{
				Passphrase:  passphrase,
				Groups:      []models.OrgDataGroup{models.OrgDataGroupAutomations},
				Conflict:    models.OrgImportConflictSkip,
				ActorUserID: src.owner,
			}, nil)
			if err != nil {
				t.Fatalf("import: %v", err)
			}

			integrations := repository.NewIntegrationRepository(pool)
			rekeyedCipher, err := rekeyed.Cipher(ctx, src.org)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range []struct {
				id       uuid.UUID
				provider models.IntegrationProvider
				secret   string
			}{{sealedConn, models.IntegrationCalCom, sealedSecret}, {clearConn, models.IntegrationClay, clearSecret}} {
				var org uuid.UUID
				var status string
				var plain, sealed, digest *string
				if err := pool.QueryRow(ctx, `
					SELECT organization_id, status, inbound_secret, inbound_secret_encrypted, inbound_secret_digest
					FROM integration_connections WHERE id = $1`, c.id).Scan(&org, &status, &plain, &sealed, &digest); err != nil {
					t.Fatalf("read imported connection: %v", err)
				}
				if org != src.org {
					t.Fatal("imported connection is not in its workspace")
				}
				found, err := integrations.GetConnectionByInboundSecret(ctx, c.provider, c.secret)
				if err != nil {
					t.Fatal(err)
				}
				if passphrase == "" {
					if status != string(models.IntegrationStatusReauthRequired) || plain != nil || sealed != nil || digest != nil {
						t.Fatalf("uncarried inbound URL not closed: status=%s", status)
					}
					if found != nil {
						t.Fatal("an uncarried inbound URL still resolves")
					}
					if !hasWarning(result.Warnings, string(c.provider)) {
						t.Fatalf("import did not name the %s connection for reconnection", c.provider)
					}
					continue
				}
				if status != string(models.IntegrationStatusConnected) {
					t.Fatalf("carried connection arrived %s", status)
				}
				if sealed == nil || plain == nil || *plain != c.secret || digest == nil || *digest != repository.InboundSecretDigest(c.secret) {
					t.Fatal("imported connection is missing its sealed secret, clear copy or digest")
				}
				if opened, err := rekeyedCipher.Decrypt(ctx, *sealed); err != nil || opened != c.secret {
					t.Fatal("imported secret does not open under the destination key")
				}
				if _, err := srcCipher.Decrypt(ctx, *sealed); err == nil {
					t.Fatal("imported secret still opens under the source key")
				}
				if found == nil || found.ID != c.id || found.OrganizationID != src.org {
					t.Fatal("imported inbound URL does not resolve to the destination connection")
				}
			}
		})
	}
}

// archiveConnectionRows returns the archive's integration_connections rows, failing if a secret appears anywhere in it.
func archiveConnectionRows(t *testing.T, archive []byte, secrets ...string) []map[string]json.RawMessage {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	for _, entry := range zr.File {
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range secrets {
			if bytes.Contains(data, []byte(s)) {
				t.Fatalf("%s carries an inbound secret in the clear", entry.Name)
			}
		}
		if entry.Name != dataPath("integration_connections") {
			continue
		}
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var row map[string]json.RawMessage
			if err := json.Unmarshal(line, &row); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func hasWarning(warnings []string, needle string) bool {
	for _, w := range warnings {
		if strings.Contains(w, needle) {
			return true
		}
	}
	return false
}

// A connection whose config did not travel is flagged once and its inbound URL closes in every column, sealed or not.
func TestLiveImportFlagsEachIntegrationOnceAndClosesItsInboundURL(t *testing.T) {
	for _, passphrase := range []string{"inbound secret archive passphrase", ""} {
		name := "with passphrase"
		if passphrase == "" {
			name = "without passphrase"
		}
		t.Run(name, func(t *testing.T) {
			keys := perOrgKeys{}
			svc, pool := keyedImportService(t, keys)
			ctx := context.Background()
			src := newTenantFixture(t, pool)
			keys[src.org] = staticKey(1)

			secret := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0x33}, 24))
			conn := uuid.New()
			// The config is sealed under a key this workspace does not hold, so the export cannot carry it;
			// the inbound secret is sealed under its own key, so with a passphrase it travels.
			if _, err := pool.Exec(ctx, `
				INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, config_encrypted, inbound_secret, inbound_secret_encrypted, inbound_secret_digest, health)
				VALUES ($1, $2, 'cal_com', 'Bookings', 'connected', 'webhook', $3, $4, $5, $6, 'healthy')`,
				conn, src.org, []byte(sealFixture(t, staticKey(9), `{"inbound_signing_key":"fixture-signing-key"}`)),
				secret, sealFixture(t, keys[src.org], secret), repository.InboundSecretDigest(secret)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM integration_connections WHERE organization_id = $1`, src.org)
			})

			groups := []models.OrgDataGroup{models.OrgDataGroupAutomations}
			var archive bytes.Buffer
			if _, err := svc.ExportTo(ctx, src.org, ExportOptions{Passphrase: passphrase, Groups: groups}, &archive, nil); err != nil {
				t.Fatalf("export: %v", err)
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

			assertInboundClosed(t, pool, conn, secret)
			var named []string
			for _, w := range result.Warnings {
				if strings.Contains(w, "cal_com (Bookings)") {
					named = append(named, w)
				}
			}
			if len(named) != 1 || !strings.Contains(named[0], "without its credentials or inbound URL") {
				t.Fatalf("want one warning naming the connection's credentials and inbound URL, got %d", len(named))
			}
		})
	}
}

// The flag step clears the sealed copy and digest itself, also on a row whose clear column is already NULL and fires no trigger.
func TestLiveReconnectFlagClearsSealedInboundSecret(t *testing.T) {
	_, pool := liveImportService(t)
	ctx := context.Background()
	src := newTenantFixture(t, pool)
	secret := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0x44}, 24))
	conn := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, config_encrypted, inbound_secret, inbound_secret_encrypted, inbound_secret_digest, health)
		VALUES ($1, $2, 'cal_com', '', 'connected', 'webhook', '\x'::bytea, NULL, $3, $4, 'healthy')`,
		conn, src.org, sealFixture(t, staticKey(1), secret), repository.InboundSecretDigest(secret)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM integration_connections WHERE id = $1`, conn)
	})
	if found, err := repository.NewIntegrationRepository(pool).GetConnectionByInboundSecret(ctx, models.IntegrationCalCom, secret); err != nil || found == nil {
		t.Fatalf("fixture URL does not resolve before the flag: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	stale, err := repository.NewOrgTransferRepository(&db.DB{Pool: pool}).MarkIntegrationsNeedingReconnect(ctx, tx, src.org, integrationReconnectDetail, inboundReconnectDetail)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || !stale[0].CredentialsMissing || !stale[0].InboundClosed {
		t.Fatalf("want the connection flagged once with its inbound URL closed, got %+v", stale)
	}
	assertInboundClosed(t, pool, conn, secret)
}

// assertInboundClosed checks a connection awaits reconnection and its old inbound URL is refused.
func assertInboundClosed(t *testing.T, pool *pgxpool.Pool, conn uuid.UUID, secret string) {
	t.Helper()
	ctx := context.Background()
	var status string
	var plain, sealed, digest *string
	if err := pool.QueryRow(ctx, `
		SELECT status, inbound_secret, inbound_secret_encrypted, inbound_secret_digest
		FROM integration_connections WHERE id = $1`, conn).Scan(&status, &plain, &sealed, &digest); err != nil {
		t.Fatal(err)
	}
	if status != string(models.IntegrationStatusReauthRequired) {
		t.Fatalf("flagged connection is %s, want reauth_required", status)
	}
	if plain != nil || sealed != nil || digest != nil {
		t.Fatalf("closed inbound URL kept a value: clear=%t sealed=%t digest=%t", plain != nil, sealed != nil, digest != nil)
	}
	found, err := repository.NewIntegrationRepository(pool).GetConnectionByInboundSecret(ctx, models.IntegrationCalCom, secret)
	if err != nil {
		t.Fatal(err)
	}
	if found != nil {
		t.Fatal("a closed inbound URL still resolves")
	}
}

// Overwriting a connection the previous release rotated in the clear still names it when its inbound URL did not travel.
func TestLiveOverwriteImportFlagsUncarriedInboundURL(t *testing.T) {
	keys := perOrgKeys{}
	svc, pool := keyedImportService(t, keys)
	ctx := context.Background()
	src := newTenantFixture(t, pool)
	keys[src.org] = staticKey(1)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM integration_connections WHERE organization_id = $1`, src.org)
	})

	secret := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0x66}, 24))
	conn := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, inbound_secret, inbound_secret_encrypted, inbound_secret_digest, health)
		VALUES ($1, $2, 'cal_com', 'Bookings', 'connected', 'webhook', $3, $4, $5, 'healthy')`,
		conn, src.org, secret, sealFixture(t, keys[src.org], secret), repository.InboundSecretDigest(secret)); err != nil {
		t.Fatal(err)
	}
	groups := []models.OrgDataGroup{models.OrgDataGroupAutomations}
	var archive bytes.Buffer
	if _, err := svc.ExportTo(ctx, src.org, ExportOptions{Groups: groups}, &archive, nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	// The previous release rotates in the clear, which leaves the row unsealed until the sweep reaches it.
	rotated := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0x77}, 24))
	if _, err := pool.Exec(ctx, `UPDATE integration_connections SET inbound_secret = $2 WHERE id = $1`, conn, rotated); err != nil {
		t.Fatal(err)
	}
	var digest *string
	if err := pool.QueryRow(ctx, `SELECT inbound_secret_digest FROM integration_connections WHERE id = $1`, conn).Scan(&digest); err != nil || digest != nil {
		t.Fatalf("fixture row is not clear-only: %v", err)
	}

	result, err := svc.ImportFrom(ctx, src.org, bytes.NewReader(archive.Bytes()), ImportOptions{
		Groups: groups, Conflict: models.OrgImportConflictOverwrite, ActorUserID: src.owner,
	}, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	assertInboundClosed(t, pool, conn, rotated)
	if !hasWarning(result.Warnings, "cal_com (Bookings)") {
		t.Fatal("import did not name the overwritten connection for reconnection")
	}
}
