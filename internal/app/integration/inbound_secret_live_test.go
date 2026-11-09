package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Inbound URL secrets are sealed with the organization DEK and found by digest.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/app/integration/ -run TestLiveInboundSecret -v

const inboundFixtureSigningKey = "fixture-signing-key"

type inboundFixture struct {
	pool *pgxpool.Pool
	svc  *service
	org  uuid.UUID
	user uuid.UUID
}

func newInboundFixture(t *testing.T) inboundFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	f := inboundFixture{pool: pool, org: uuid.New(), user: uuid.New()}
	tag := f.org.String()[:8]
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Inbound', 'Live', $2, 'x')`,
			[]any{f.user, "inbound-" + tag + "@test.local"}},
		{`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Inbound', $2, $3)`,
			[]any{f.org, "inbound-" + tag, f.user}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM integration_connections WHERE organization_id = $1`, f.org)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, f.org)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.user)
	})
	key := bytes.Repeat([]byte{7}, 32)
	f.svc = NewService(repository.NewIntegrationRepository(pool), cipher.NewStatic(key), nil).(*service)
	return f
}

// deliver runs a signed Cal.com delivery through the lookup and signature check the inbound handler uses.
func (f inboundFixture) deliver(t *testing.T, secret string) (uuid.UUID, bool) {
	t.Helper()
	ctx := context.Background()
	conn, err := f.svc.Repo().GetConnectionByInboundSecret(ctx, models.IntegrationCalCom, secret)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if conn == nil {
		return uuid.Nil, false
	}
	body := []byte(`{"triggerEvent":"BOOKING_CREATED"}`)
	mac := hmac.New(sha256.New, []byte(inboundFixtureSigningKey))
	mac.Write(body)
	header := func(name string) string {
		if name == "X-Cal-Signature-256" {
			return hex.EncodeToString(mac.Sum(nil))
		}
		return ""
	}
	key, err := f.svc.InboundSigningKey(ctx, conn)
	if err != nil {
		t.Fatalf("signing key: %v", err)
	}
	if key == "" || !VerifyInboundSignature(models.IntegrationCalCom, key, header, body, time.Now()) {
		t.Fatal("signed delivery did not verify")
	}
	return conn.ID, true
}

type inboundColumns struct {
	plain, sealed, digest *string
}

func (f inboundFixture) columns(t *testing.T, id uuid.UUID) inboundColumns {
	t.Helper()
	var c inboundColumns
	if err := f.pool.QueryRow(context.Background(), `
		SELECT inbound_secret, inbound_secret_encrypted, inbound_secret_digest
		FROM integration_connections WHERE id = $1`, id).Scan(&c.plain, &c.sealed, &c.digest); err != nil {
		t.Fatalf("read connection: %v", err)
	}
	return c
}

// assertSealed checks the row holds secret sealed under the org DEK, its digest, and the clear copy the previous release reads.
func (f inboundFixture) assertSealed(t *testing.T, id uuid.UUID, secret string) {
	t.Helper()
	c := f.columns(t, id)
	if c.sealed == nil || *c.sealed == "" || *c.sealed == secret {
		t.Fatal("inbound secret has no sealed copy")
	}
	opened, err := f.svc.open(context.Background(), f.org, *c.sealed)
	if err != nil || opened != secret {
		t.Fatalf("sealed copy does not open to the secret (err %v)", err)
	}
	if c.digest == nil || *c.digest != repository.InboundSecretDigest(secret) {
		t.Fatal("digest does not match the secret")
	}
	if c.plain == nil || *c.plain != secret {
		t.Fatal("clear copy for the previous release is missing")
	}
}

func (f inboundFixture) connectCalCom(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	conn, err := f.svc.Connect(ctx, f.org, f.user, models.IntegrationCalCom, "Bookings", nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := f.svc.SetInboundSigningKey(ctx, f.org, conn.ID, inboundFixtureSigningKey); err != nil {
		t.Fatalf("set signing key: %v", err)
	}
	return conn.ID, path.Base(conn.InboundWebhookURL)
}

func TestLiveInboundSecretSealedAcrossWriteBackfillAndRead(t *testing.T) {
	f := newInboundFixture(t)
	ctx := context.Background()

	id, secret := f.connectCalCom(t)
	f.assertSealed(t, id, secret)
	if got, ok := f.deliver(t, secret); !ok || got != id {
		t.Fatal("delivery to a freshly minted URL did not resolve")
	}

	url, err := f.svc.RotateInboundSecret(ctx, f.org, id)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	rotated := path.Base(url)
	f.assertSealed(t, id, rotated)
	if _, ok := f.deliver(t, secret); ok {
		t.Fatal("rotated-out URL still resolves")
	}
	if got, ok := f.deliver(t, rotated); !ok || got != id {
		t.Fatal("rotated URL did not resolve")
	}

	// The previous release rotates by writing only the clear column; the sealed copy retires with it.
	prior := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0xab}, 24))
	if _, err := f.pool.Exec(ctx, `UPDATE integration_connections SET inbound_secret = $2 WHERE id = $1`, id, prior); err != nil {
		t.Fatal(err)
	}
	if c := f.columns(t, id); c.sealed != nil || c.digest != nil {
		t.Fatal("a clear-only rotation left the earlier sealed copy in place")
	}
	if _, ok := f.deliver(t, rotated); ok {
		t.Fatal("URL rotated out by the previous release still resolves")
	}
	if got, ok := f.deliver(t, prior); !ok || got != id {
		t.Fatal("URL minted by the previous release did not resolve before sealing")
	}

	if _, err := f.svc.SealInboundSecrets(ctx); err != nil {
		t.Fatalf("seal pass: %v", err)
	}
	f.assertSealed(t, id, prior)
	if got, ok := f.deliver(t, prior); !ok || got != id {
		t.Fatal("URL did not resolve after sealing")
	}
}

func TestLiveInboundSecretUnsealedRowStillResolves(t *testing.T) {
	f := newInboundFixture(t)
	ctx := context.Background()

	id, _ := f.connectCalCom(t)
	// A row as the previous release writes it: the secret in the clear and nothing else.
	legacy := "calcom_" + hex.EncodeToString(bytes.Repeat([]byte{0xcd}, 24))
	if _, err := f.pool.Exec(ctx, `
		UPDATE integration_connections
		SET inbound_secret = $2, inbound_secret_encrypted = NULL, inbound_secret_digest = NULL
		WHERE id = $1`, id, legacy); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.deliver(t, legacy); !ok || got != id {
		t.Fatal("unsealed row did not resolve")
	}

	if _, err := f.svc.SealInboundSecrets(ctx); err != nil {
		t.Fatalf("seal pass: %v", err)
	}
	f.assertSealed(t, id, legacy)
	if got, ok := f.deliver(t, legacy); !ok || got != id {
		t.Fatal("row did not resolve after sealing")
	}
	// A second pass finds nothing left to seal on this row and changes nothing.
	before := f.columns(t, id)
	if _, err := f.svc.SealInboundSecrets(ctx); err != nil {
		t.Fatalf("second seal pass: %v", err)
	}
	if after := f.columns(t, id); *after.sealed != *before.sealed {
		t.Fatal("a second pass resealed an already sealed row")
	}
}
