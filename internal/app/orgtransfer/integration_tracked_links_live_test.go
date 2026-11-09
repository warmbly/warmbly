package orgtransfer

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// A workspace archive carries its integration credentials re-keyed for the
// destination, flags the ones it could not carry, and carries the click
// tickets of campaign and direct sends alike, never another workspace's.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/app/orgtransfer/ -run LiveArchiveCarries -v

// perOrgKeys gives every organization its own DEK, as two instances would.
type perOrgKeys map[uuid.UUID]cipher.CipherService

func (k perOrgKeys) Cipher(ctx context.Context, orgID uuid.UUID) (*cipher.Cipher, error) {
	keys, ok := k[orgID]
	if !ok {
		return nil, errors.New("no key for this organization")
	}
	return keys.Cipher(ctx, orgID)
}

func keyedImportService(t *testing.T, keys perOrgKeys) (Service, *pgxpool.Pool) {
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
	return NewService(repository.NewOrgTransferRepository(handle), keys, nil, nil, InstanceInfo{}), handle.Pool
}

func staticKey(b byte) cipher.CipherService { return cipher.NewStatic(bytes.Repeat([]byte{b}, 32)) }

func sealFixture(t *testing.T, keys cipher.CipherService, plain string) string {
	t.Helper()
	c, err := keys.Cipher(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Encrypt(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// archiveRows reads one table's rows out of an archive.
func archiveRows(t *testing.T, archive []byte, table string) []map[string]any {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, f := range zr.File {
		if f.Name != dataPath(table) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var row map[string]any
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			out = append(out, row)
		}
		_ = rc.Close()
	}
	return out
}

type linkFixture struct {
	mailbox, directTask, campaignTask, directLink, campaignLink uuid.UUID
}

func seedLinks(t *testing.T, pool *pgxpool.Pool, f tenantFixture) linkFixture {
	t.Helper()
	ctx := context.Background()
	l := linkFixture{mailbox: uuid.New(), directTask: uuid.New(), campaignTask: uuid.New(), directLink: uuid.New(), campaignLink: uuid.New()}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider) VALUES($1,$2,$3,$4,'Links','','','smtp_imap')`,
			[]any{l.mailbox, f.owner, f.org, l.mailbox.String() + "@example.test"}},
		{`INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'email','completed','')`, []any{l.directTask, l.mailbox}},
		{`INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'campaign','completed','')`, []any{l.campaignTask, l.mailbox}},
		{`INSERT INTO tracked_links(id,task_id,campaign_id,destination,label) VALUES($1,$2,$3,'https://example.test/direct','Direct')`, []any{l.directLink, l.directTask, uuid.Nil}},
		{`INSERT INTO tracked_links(id,task_id,campaign_id,destination,label) VALUES($1,$2,$3,'https://example.test/campaign','Campaign')`, []any{l.campaignLink, l.campaignTask, f.campaign}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("seed links: %v", err)
		}
	}
	t.Cleanup(func() { dropLinks(pool, l) })
	return l
}

func dropLinks(pool *pgxpool.Pool, l linkFixture) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM tracked_links WHERE id = ANY($1)`, []uuid.UUID{l.directLink, l.campaignLink})
	_, _ = pool.Exec(ctx, `DELETE FROM email_accounts WHERE id = $1`, l.mailbox)
}

type connFixture struct{ apollo, hubspot, calendly uuid.UUID }

// Plaintexts the fixture seals. Assertions compare them and never print them.
const (
	apolloConfig   = `{"api_key":"fixture-apollo-key"}`
	hubspotAccess  = "fixture-hubspot-access"
	hubspotRefresh = "fixture-hubspot-refresh"
)

func seedConnections(t *testing.T, pool *pgxpool.Pool, org uuid.UUID, keys cipher.CipherService) connFixture {
	t.Helper()
	ctx := context.Background()
	c := connFixture{apollo: uuid.New(), hubspot: uuid.New(), calendly: uuid.New()}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		// config_encrypted is bytea and holds the sealed text's bytes, as integration.sealConfig writes it.
		{`INSERT INTO integration_connections(id,organization_id,provider,label,status,auth_method,config_encrypted,health) VALUES($1,$2,'apollo','Prospecting','connected','api_key',$3,'healthy')`,
			[]any{c.apollo, org, []byte(sealFixture(t, keys, apolloConfig))}},
		{`INSERT INTO integration_connections(id,organization_id,provider,label,status,auth_method,access_token_encrypted,refresh_token_encrypted,health) VALUES($1,$2,'hubspot','','connected','oauth',$3,$4,'healthy')`,
			[]any{c.hubspot, org, sealFixture(t, keys, hubspotAccess), sealFixture(t, keys, hubspotRefresh)}},
		// Sealed under a key this workspace does not hold, so it cannot be opened at export.
		{`INSERT INTO integration_connections(id,organization_id,provider,label,status,auth_method,inbound_secret,config_encrypted,health) VALUES($1,$2,'calendly','Bookings','connected','webhook',$3,$4,'healthy')`,
			[]any{c.calendly, org, "in_" + c.calendly.String(), []byte(sealFixture(t, staticKey(9), `{"inbound_signing_key":"fixture-signing-key"}`))}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("seed connections: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM integration_connections WHERE id = ANY($1)`, []uuid.UUID{c.apollo, c.hubspot, c.calendly})
	})
	return c
}

func openFixture(t *testing.T, keys cipher.CipherService, sealed string) string {
	t.Helper()
	c, err := keys.Cipher(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.Decrypt(context.Background(), sealed)
	if err != nil {
		t.Fatalf("destination key does not open the imported credential: %v", err)
	}
	return plain
}

func TestLiveArchiveCarriesIntegrationCredentialsAndDirectMailLinks(t *testing.T) {
	for _, withPassphrase := range []bool{true, false} {
		name := "with_passphrase"
		if !withPassphrase {
			name = "without_passphrase"
		}
		t.Run(name, func(t *testing.T) {
			keys := perOrgKeys{}
			svc, pool := keyedImportService(t, keys)
			ctx := context.Background()

			source := newTenantFixture(t, pool)
			other := newTenantFixture(t, pool)
			dest := newTenantFixture(t, pool)
			keys[source.org], keys[other.org], keys[dest.org] = staticKey(1), staticKey(2), staticKey(3)

			srcLinks := seedLinks(t, pool, source)
			otherLinks := seedLinks(t, pool, other)
			srcConns := seedConnections(t, pool, source.org, keys[source.org])
			otherConns := seedConnections(t, pool, other.org, keys[other.org])

			groups := []models.OrgDataGroup{models.OrgDataGroupCore, models.OrgDataGroupContacts, models.OrgDataGroupCampaigns, models.OrgDataGroupAutomations, models.OrgDataGroupSending}
			opts := ExportOptions{Groups: groups}
			if withPassphrase {
				opts.Passphrase = "correct horse battery staple"
			}
			var buf bytes.Buffer
			if _, err := svc.ExportTo(ctx, source.org, opts, &buf, nil); err != nil {
				t.Fatal(err)
			}
			archive := buf.Bytes()

			var linkIDs []string
			for _, r := range archiveRows(t, archive, "tracked_links") {
				linkIDs = append(linkIDs, r["id"].(string))
			}
			slices.Sort(linkIDs)
			want := []string{srcLinks.directLink.String(), srcLinks.campaignLink.String()}
			slices.Sort(want)
			if !slices.Equal(linkIDs, want) {
				t.Fatalf("archive tracked_links = %v, want the source's direct and campaign links %v", linkIDs, want)
			}
			for _, r := range archiveRows(t, archive, "integration_connections") {
				if id := r["id"].(string); id == otherConns.apollo.String() || id == otherConns.hubspot.String() || id == otherConns.calendly.String() {
					t.Fatal("archive carries another workspace's integration")
				}
			}
			for _, plain := range []string{apolloConfig, hubspotAccess, hubspotRefresh, "fixture-signing-key"} {
				if bytes.Contains(archive, []byte(plain)) {
					t.Fatal("archive carries a credential in the clear")
				}
			}

			// The destination is a different workspace under a different key; the
			// source's rows leave first, as they would never exist on another instance.
			if _, err := pool.Exec(ctx, `DELETE FROM integration_connections WHERE organization_id = $1`, source.org); err != nil {
				t.Fatal(err)
			}
			dropLinks(pool, srcLinks)
			for _, q := range []string{
				`DELETE FROM campaigns WHERE organization_id = $1`,
				`DELETE FROM contacts WHERE organization_id = $1`,
				`DELETE FROM organization_members WHERE organization_id = $1`,
			} {
				if _, err := pool.Exec(ctx, q, source.org); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM integration_connections WHERE organization_id = $1`, dest.org)
			})

			result, err := svc.ImportFrom(ctx, dest.org, bytes.NewReader(archive), ImportOptions{
				Groups: groups, Conflict: models.OrgImportConflictOverwrite, Passphrase: opts.Passphrase, ActorUserID: dest.owner,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}

			type conn struct {
				org                      uuid.UUID
				status, health           string
				config                   []byte
				access, refresh, inbound *string
			}
			read := func(id uuid.UUID) conn {
				t.Helper()
				var c conn
				if err := pool.QueryRow(ctx, `SELECT organization_id, status, health, config_encrypted, access_token_encrypted, refresh_token_encrypted, inbound_secret FROM integration_connections WHERE id = $1`, id).
					Scan(&c.org, &c.status, &c.health, &c.config, &c.access, &c.refresh, &c.inbound); err != nil {
					t.Fatal(err)
				}
				return c
			}

			var reconnect []string
			for _, w := range result.Warnings {
				if strings.Contains(w, "integration arrived without its credentials") {
					reconnect = append(reconnect, w)
				}
			}
			// Each connection is named once, whatever of its credentials and inbound URL stayed behind.
			for _, name := range []string{"apollo (Prospecting)", "The hubspot integration", "calendly (Bookings)"} {
				n := 0
				for _, w := range result.Warnings {
					if strings.Contains(w, name) {
						n++
					}
				}
				if n > 1 {
					t.Fatalf("%s is named in %d import warnings, want one", name, n)
				}
			}

			apollo, hubspot, calendly := read(srcConns.apollo), read(srcConns.hubspot), read(srcConns.calendly)
			for _, c := range []conn{apollo, hubspot, calendly} {
				if c.org != dest.org {
					t.Fatal("imported connection does not belong to the destination workspace")
				}
			}
			if calendly.status != "reauth_required" || calendly.health != "down" || calendly.config != nil || calendly.inbound != nil {
				t.Fatalf("uncarried credential not flagged: status=%s health=%s config=%t inbound=%t", calendly.status, calendly.health, calendly.config != nil, calendly.inbound != nil)
			}
			if withPassphrase {
				if apollo.status != "connected" || hubspot.status != "connected" {
					t.Fatalf("carried connections changed state: apollo=%s hubspot=%s", apollo.status, hubspot.status)
				}
				if openFixture(t, keys[dest.org], string(apollo.config)) != apolloConfig ||
					hubspot.access == nil || openFixture(t, keys[dest.org], *hubspot.access) != hubspotAccess ||
					hubspot.refresh == nil || openFixture(t, keys[dest.org], *hubspot.refresh) != hubspotRefresh {
					t.Fatal("imported credentials do not open to the source's values under the destination key")
				}
				if len(reconnect) != 1 || !strings.Contains(reconnect[0], "calendly (Bookings)") {
					t.Fatalf("want one reconnect warning naming calendly, got %d", len(reconnect))
				}
			} else {
				for name, c := range map[string]conn{"apollo": apollo, "hubspot": hubspot} {
					if c.status != "reauth_required" || c.config != nil || c.access != nil || c.refresh != nil {
						t.Fatalf("%s arrived without credentials but was not flagged: status=%s", name, c.status)
					}
				}
				if len(reconnect) != 3 {
					t.Fatalf("want a reconnect warning per connection, got %d", len(reconnect))
				}
			}

			// The other workspace keeps its connections and credentials as they were.
			for _, id := range []uuid.UUID{otherConns.apollo, otherConns.hubspot, otherConns.calendly} {
				if c := read(id); c.org != other.org || c.status != "connected" {
					t.Fatalf("another workspace's connection changed: status=%s", c.status)
				}
			}
			if c := read(otherConns.apollo); openFixture(t, keys[other.org], string(c.config)) != apolloConfig {
				t.Fatal("another workspace's credential changed")
			}

			// Both tickets resolve to the destination, on a tracking domain it holds.
			links := repository.NewTrackedLinkRepository(pool)
			host := "links-" + dest.org.String()[:8] + ".example.test"
			if _, err := pool.Exec(ctx, `UPDATE email_accounts SET tracking_domain = $2, tracking_domain_verified = true WHERE id = $1 AND organization_id = $3`, srcLinks.mailbox, host, dest.org); err != nil {
				t.Fatal(err)
			}
			owners, restricted, err := customhost.New(repository.NewCustomDomainRepository(pool), nil, nil).Guard(ctx, host)
			if err != nil || !restricted {
				t.Fatalf("destination tracking domain not served: restricted=%t err=%v", restricted, err)
			}
			for _, id := range []uuid.UUID{srcLinks.directLink, srcLinks.campaignLink} {
				link, err := links.GetByID(ctx, id)
				if err != nil || link == nil {
					t.Fatalf("imported ticket does not resolve: %v", err)
				}
				if !customhost.Allowed(owners, link.OrganizationID) {
					t.Fatal("imported ticket is refused on the destination's tracking domain")
				}
			}
			foreign, err := links.GetByID(ctx, otherLinks.directLink)
			if err != nil || foreign == nil || foreign.OrganizationID == nil || *foreign.OrganizationID != other.org {
				t.Fatalf("another workspace's ticket moved: %v", err)
			}
			if customhost.Allowed(owners, foreign.OrganizationID) {
				t.Fatal("another workspace's ticket resolves on the destination's tracking domain")
			}
		})
	}
}
