package email

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live tracking-domain checks against a real Postgres and real DNS. Skipped
// unless WARMBLY_TEST_DB is set, so `go test ./...` stays hermetic:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/email/ -run Live -v
//
// The bug these exist for is not in the logic: it is in the WHERE clause. The
// tracking-domain write filtered on user_id while the read filtered on
// organization_id, which no unit test with a stubbed repository can see.

func liveTrackingPool(t *testing.T) *pgxpool.Pool {
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
	return handle.Pool
}

type trackingFixture struct {
	pool    *pgxpool.Pool
	user    uuid.UUID
	org     uuid.UUID
	mailbox uuid.UUID
	svc     EmailService
}

func newTrackingFixture(t *testing.T) *trackingFixture {
	t.Helper()
	pool := liveTrackingPool(t)
	ctx := context.Background()

	f := &trackingFixture{pool: pool, user: uuid.New(), org: uuid.New(), mailbox: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Track', 'Test')`,
		f.user, "track-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Track Test', $2, $3)`,
		f.org, "track-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time)
	      VALUES ($1, $2, $3, $4, 'Track', '', '', 'smtp_imap', 'active', 50, 600)`,
		f.mailbox, f.user, f.org, "track-"+f.mailbox.String()[:8]+"@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	handle := &db.DB{Pool: pool}
	f.svc = NewService(repository.NewEmailRepostory(handle, nil), nil, nil, nil, nil)
	prover := domainproof.New("live-test")
	f.svc.(CustomHostsAware).WireCustomHosts(customhost.New(nil, prover, publishedProof{prover, f.org}))
	return f
}

// publishedProof answers every TXT lookup with the workspace's proof, as if the owner had added the record.
type publishedProof struct {
	prover *domainproof.Prover
	org    uuid.UUID
}

func (p publishedProof) LookupTXT(_ context.Context, name string) ([]string, error) {
	return []string{p.prover.Value(p.org, strings.TrimPrefix(name, domainproof.Label+"."))}, nil
}

func (f *trackingFixture) stored(t *testing.T) (string, bool) {
	t.Helper()
	var domain string
	var verified bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT tracking_domain, tracking_domain_verified FROM email_accounts WHERE id = $1`,
		f.mailbox).Scan(&domain, &verified); err != nil {
		t.Fatalf("read back: %v", err)
	}
	return domain, verified
}

// The write is scoped by organization, like the read. It used to be scoped by
// user_id, so any teammate holding manage_emails got a 404 instead of a save.
func TestLiveTrackingDomainWriteIsOrgScoped(t *testing.T) {
	f := newTrackingFixture(t)
	ctx := context.Background()

	t.Setenv("TRACKING_DOMAIN", "dyna.wikimedia.org")

	status, xerr := f.svc.UpdateTrackingDomain(ctx, f.org.String(), f.mailbox.String(), "track.acme-does-not-exist-warmbly.com")
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if status.CNAMETarget != "dyna.wikimedia.org" {
		t.Fatalf("the CNAME target has to come from this install: %+v", status)
	}
	if status.Status != trackdns.CodeNotFound || status.Message == "" {
		t.Fatalf("an unregistered name should report not_found with a reason: %+v", status)
	}

	if domain, verified := f.stored(t); domain != "track.acme-does-not-exist-warmbly.com" || verified {
		t.Fatalf("stored %q verified=%v", domain, verified)
	}

	// A different organization must not be able to touch it.
	_, err := f.svc.UpdateTrackingDomain(ctx, uuid.New().String(), f.mailbox.String(), "elsewhere.example.com")
	if err == nil {
		t.Fatalf("another organization must not be able to write this mailbox")
	}
	if domain, _ := f.stored(t); domain != "track.acme-does-not-exist-warmbly.com" {
		t.Fatalf("the foreign write changed the row: %q", domain)
	}
}

// Save, read back, re-verify: the three calls the drawer makes, against real
// DNS. www.wikipedia.org is a real CNAME to dyna.wikimedia.org.
func TestLiveTrackingDomainVerifyRoundTrip(t *testing.T) {
	f := newTrackingFixture(t)
	ctx := context.Background()

	t.Setenv("TRACKING_DOMAIN", "dyna.wikimedia.org")

	status, xerr := f.svc.UpdateTrackingDomain(ctx, f.org.String(), f.mailbox.String(), "https://WWW.Wikipedia.org/")
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if status.TrackingDomain != "www.wikipedia.org" {
		t.Fatalf("a pasted URL should be stored as the bare host, got %q", status.TrackingDomain)
	}
	if !status.TrackingDomainVerified || status.Status != trackdns.CodeVerified {
		t.Fatalf("a real CNAME to the tracking host should verify: %+v", status)
	}
	if domain, verified := f.stored(t); domain != "www.wikipedia.org" || !verified {
		t.Fatalf("stored %q verified=%v", domain, verified)
	}

	got, xerr := f.svc.GetTrackingDomain(ctx, f.org.String(), f.mailbox.String())
	if xerr != nil {
		t.Fatalf("get: %v", xerr)
	}
	if !got.TrackingDomainVerified || got.CNAMETarget != "dyna.wikimedia.org" {
		t.Fatalf("read back: %+v", got)
	}

	again, xerr := f.svc.VerifyTrackingDomain(ctx, f.org.String(), f.mailbox.String())
	if xerr != nil {
		t.Fatalf("verify: %v", xerr)
	}
	if !again.TrackingDomainVerified {
		t.Fatalf("re-verify: %+v", again)
	}
}

// A malformed value is rejected instead of being stored and left to sit at
// "pending DNS" forever, which is how a typo looked like a broken product.
func TestLiveTrackingDomainRejectsMalformed(t *testing.T) {
	f := newTrackingFixture(t)
	ctx := context.Background()
	t.Setenv("TRACKING_DOMAIN", "dyna.wikimedia.org")

	for _, bad := range []string{"not a domain", "track", "127.0.0.1", "localhost", "track.acme.com:3000"} {
		if _, err := f.svc.UpdateTrackingDomain(ctx, f.org.String(), f.mailbox.String(), bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if domain, _ := f.stored(t); domain != "" {
		t.Fatalf("nothing should have been stored, got %q", domain)
	}
}

// The sweep is the half of the fix that no button press is needed for: a
// record that propagates later gets picked up, and one that stops resolving
// stops routing links.
func TestLiveTrackingDomainSweep(t *testing.T) {
	f := newTrackingFixture(t)
	ctx := context.Background()
	t.Setenv("TRACKING_DOMAIN", "dyna.wikimedia.org")

	svc, ok := f.svc.(*emailService)
	if !ok {
		t.Fatalf("expected the concrete service")
	}

	// Stored as pending even though the record is (really) correct: this is a
	// customer who added the CNAME after saving.
	if _, err := f.pool.Exec(ctx,
		`UPDATE email_accounts SET tracking_domain = 'www.wikipedia.org',
		        tracking_domain_verified = false, tracking_domain_verified_at = NULL
		 WHERE id = $1`, f.mailbox); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := svc.runTrackingDomainSweep(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}

	if domain, verified := f.stored(t); domain != "www.wikipedia.org" || !verified {
		t.Fatalf("the sweep should have verified it without anybody asking: %q verified=%v", domain, verified)
	}

	// Now the other direction: a domain that no longer points at the tracking
	// host must stop being used, rather than routing links at it forever.
	if _, err := f.pool.Exec(ctx,
		`UPDATE email_accounts SET tracking_domain = 'www.microsoft.com',
		        tracking_domain_verified = true, tracking_domain_verified_at = NULL
		 WHERE id = $1`, f.mailbox); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := svc.runTrackingDomainSweep(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}

	if _, verified := f.stored(t); verified {
		t.Fatalf("a domain pointing somewhere else must not stay verified")
	}
}

// With no tracking host there is nothing to compare against, and unverifying
// every mailbox on an install that simply has not configured tracking would be
// the worst possible reading of "no answer".
func TestLiveTrackingDomainSweepDoesNothingWithoutATarget(t *testing.T) {
	f := newTrackingFixture(t)
	ctx := context.Background()
	t.Setenv("TRACKING_DOMAIN", "")

	svc := f.svc.(*emailService)
	if _, err := f.pool.Exec(ctx,
		`UPDATE email_accounts SET tracking_domain = 'www.wikipedia.org',
		        tracking_domain_verified = true, tracking_domain_verified_at = NULL
		 WHERE id = $1`, f.mailbox); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := svc.runTrackingDomainSweep(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}

	if _, verified := f.stored(t); !verified {
		t.Fatalf("the sweep must leave everything alone when it has no target")
	}
}
