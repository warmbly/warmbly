package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

// Admin grants, vendor connections and domain redirects are organization
// assets; every read is scoped, and a verified redirect answers for its domain
// in exactly one workspace.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<scratch>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveMailboxSources -v

func TestLiveMailboxSourcesGrants(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 206)
	f := newImportFixture(t, pool)
	grants := NewDomainGrantRepository(handle)
	emails := NewEmailRepostory(handle, nil)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mailbox_domain_grants WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, f.other})
	})

	g := &models.DomainGrant{ID: uuid.New(), OrganizationID: f.org, Provider: models.GrantProviderMicrosoft, Tenant: "tenant-1", Domains: []string{"Contoso.com"}}
	mustImport(t, grants.Upsert(ctx, g, f.owner))
	again := &models.DomainGrant{ID: uuid.New(), OrganizationID: f.org, Provider: models.GrantProviderMicrosoft, Tenant: "tenant-1", Domains: []string{"contoso.com", "fabrikam.com"}}
	mustImport(t, grants.Upsert(ctx, again, f.owner))
	if again.ID != g.ID {
		t.Fatal("a second consent for the same tenant made a second grant")
	}

	t.Run("covering domain is found, scoped", func(t *testing.T) {
		got, err := grants.ForDomain(ctx, f.org, models.GrantProviderMicrosoft, "FABRIKAM.com")
		if err != nil || got == nil || got.ID != g.ID {
			t.Fatalf("got %+v, %v", got, err)
		}
		if got, _ := grants.ForDomain(ctx, f.other, models.GrantProviderMicrosoft, "contoso.com"); got != nil {
			t.Fatal("another workspace found the grant")
		}
		if got, _ := grants.Get(ctx, f.other, g.ID); got != nil {
			t.Fatal("another workspace read the grant")
		}
	})

	t.Run("a delegated mailbox stores no credential and is found for tokens", func(t *testing.T) {
		acc, xerr := emails.NewDelegatedAccount(ctx, f.owner.String(), models.NewDelegatedAccount{
			OrganizationID: &f.org, Provider: models.InboxProviderOutlook, Email: "sam@contoso.com", Name: "Sam",
			MailHost: "microsoft365", GrantID: g.ID, Subject: "graph-user-1",
		})
		if xerr != nil {
			t.Fatal(xerr)
		}
		if acc.AuthMethod != models.MailAuthDelegated || acc.MailHost != "microsoft365" {
			t.Fatalf("account = %+v", acc)
		}
		d, xerr := emails.GetDelegation(ctx, acc.ID)
		if xerr != nil || d == nil || d.Subject != "graph-user-1" || d.GrantID != g.ID || d.OrganizationID != f.org {
			t.Fatalf("delegation = %+v, %v", d, xerr)
		}
		var oauthRows int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM email_accounts_oauth WHERE email_account_id = $1`, acc.ID).Scan(&oauthRows)
		if oauthRows != 0 {
			t.Fatal("a delegated mailbox stored a token")
		}
		if _, xerr := emails.NewDelegatedAccount(ctx, f.owner.String(), models.NewDelegatedAccount{
			OrganizationID: &f.org, Provider: models.InboxProviderOutlook, Email: "SAM@contoso.com", GrantID: g.ID, Subject: "x",
		}); xerr == nil {
			t.Fatal("the same address was connected twice")
		}
		list, err := grants.List(ctx, f.org)
		if err != nil || len(list) != 1 || list[0].Mailboxes != 1 {
			t.Fatalf("list = %+v, %v", list, err)
		}
		mustImport(t, grants.SetStatus(ctx, g.ID, "invalid", "revoked"))
		if got, _ := grants.ForDomain(ctx, f.org, models.GrantProviderMicrosoft, "contoso.com"); got != nil {
			t.Fatal("an invalid grant still covers its domain")
		}
		ids, err := grants.Delete(ctx, f.org, g.ID)
		if err != nil || len(ids) != 1 || ids[0] != acc.ID {
			t.Fatalf("deleted mailboxes = %v, %v", ids, err)
		}
		if d, _ := emails.GetDelegation(ctx, acc.ID); d != nil {
			t.Fatal("a mailbox kept minting after its grant was deleted")
		}
	})
}

func TestLiveMailboxSourcesVendors(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 206)
	f := newImportFixture(t, pool)
	vendors := NewVendorConnectionRepository(handle)
	emails := NewEmailRepostory(handle, nil)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mailbox_vendor_connections WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, f.other})
	})

	c := &models.VendorConnection{ID: uuid.New(), OrganizationID: f.org, Vendor: "inboxkit", Label: "Main", Credentials: "sealed"}
	mustImport(t, vendors.Create(ctx, c, f.owner))
	list, err := vendors.List(ctx, f.org)
	if err != nil || len(list) != 1 || list[0].Credentials != "" {
		t.Fatalf("list leaks or misses: %+v, %v", list, err)
	}
	if got, _ := vendors.Get(ctx, f.other, c.ID); got != nil {
		t.Fatal("another workspace read the connection")
	}
	if ok, _ := vendors.Update(ctx, f.org, c.ID, "Renamed", ""); !ok {
		t.Fatal("update failed")
	}
	if got, _ := vendors.Get(ctx, f.org, c.ID); got.Credentials != "sealed" || got.Label != "Renamed" {
		t.Fatalf("a rename replaced the key: %+v", got)
	}

	box := f.mailbox(t, pool, f.org, f.owner, "v@acme.io")
	if xerr := emails.SetVendorLink(ctx, box, c.ID, "ik-1"); xerr != nil {
		t.Fatal(xerr)
	}
	if _, err := pool.Exec(ctx, `UPDATE email_accounts SET status = 'inactive' WHERE id = $1`, box); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_account_errors (email_account_id, user_id, error_code, severity, title, message)
		VALUES ($1, $2, 'INVALID_CREDENTIALS', 'CRITICAL', 'Sign-in failed', 'x')`, box, f.owner); err != nil {
		t.Fatal(err)
	}
	cands, err := vendors.ReconnectCandidates(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cand := range cands {
		found = found || (cand.AccountID == box && cand.VendorMailboxID == "ik-1" && cand.OrgID == f.org)
	}
	if !found {
		t.Fatalf("the failed vendor mailbox was not offered for reconnect: %+v", cands)
	}
	if ok, _ := vendors.Delete(ctx, f.org, c.ID); !ok {
		t.Fatal("delete failed")
	}
	var link *uuid.UUID
	_ = pool.QueryRow(ctx, `SELECT vendor_connection_id FROM email_accounts WHERE id = $1`, box).Scan(&link)
	if link != nil {
		t.Fatal("the mailbox kept a link to a deleted connection")
	}
}

func TestLiveMailboxSourcesRedirects(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 206)
	f := newImportFixture(t, pool)
	redirects := NewDomainRedirectRepository(handle)
	emails := NewEmailRepostory(handle, nil)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM domain_redirects WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, f.other})
	})
	domain := "redir-" + f.org.String()[:8] + ".io"

	a := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.org, Domain: domain, TargetURL: "https://acme.com", IncludeWWW: true, VerifyToken: "tok-a"}
	mustImport(t, redirects.Upsert(ctx, a, &f.owner))
	again := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.org, Domain: domain, TargetURL: "https://acme.com/new", IncludeWWW: true, VerifyToken: "tok-new"}
	mustImport(t, redirects.Upsert(ctx, again, &f.owner))
	if again.VerifyToken != "tok-a" || again.TargetURL != "https://acme.com/new" {
		t.Fatalf("an update rotated the token or kept the old target: %+v", again)
	}
	if _, ok, _ := redirects.Lookup(ctx, domain); ok {
		t.Fatal("an unverified redirect is served")
	}
	mustImport(t, redirects.SetCheck(ctx, a.ID, true, ""))
	for _, host := range []string{domain, "www." + domain, domain + "."} {
		if target, ok, err := redirects.Lookup(ctx, host); err != nil || !ok || target != "https://acme.com/new" {
			t.Fatalf("%s -> %q, %v, %v", host, target, ok, err)
		}
	}
	custom := NewCustomDomainRepository(pool)
	if ok, err := custom.IsVerified(ctx, "www."+domain); err != nil || !ok {
		t.Fatalf("certificate issuance refused the verified redirect: %v", err)
	}

	b := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: domain, TargetURL: "https://evil.com", VerifyToken: "tok-b"}
	mustImport(t, redirects.Upsert(ctx, b, &f.owner))
	if err := redirects.SetCheck(ctx, b.ID, true, ""); !errors.Is(err, ErrRedirectTaken) {
		t.Fatalf("a second workspace verified the same domain: %v", err)
	}
	if target, _, _ := redirects.Lookup(ctx, domain); target != "https://acme.com/new" {
		t.Fatalf("the redirect was taken over: %q", target)
	}

	t.Run("domain overview and bulk tracking", func(t *testing.T) {
		f.mailbox(t, pool, f.org, f.owner, "one@"+domain)
		f.mailbox(t, pool, f.org, f.teammate, "two@"+domain)
		n, xerr := emails.SetDomainTracking(ctx, f.org, domain, "track."+domain, false, nil)
		if xerr != nil || n != 2 {
			t.Fatalf("updated %d, %v", n, xerr)
		}
		if n, _ := emails.CountDomainMailboxes(ctx, f.other, domain); n != 0 {
			t.Fatal("another workspace counted the domain's mailboxes")
		}
		list, xerr := emails.DomainsOverview(ctx, f.org)
		if xerr != nil {
			t.Fatal(xerr)
		}
		for _, d := range list {
			if d.Domain == domain {
				if d.Mailboxes != 2 || len(d.TrackingDomains) != 1 || d.TrackingDomains[0].Host != "track."+domain {
					t.Fatalf("overview = %+v", d)
				}
				return
			}
		}
		t.Fatal("the domain is missing from the overview")
	})
}

func TestLiveDomainRedirectServing(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 238)
	f := newImportFixture(t, pool)
	redirects := NewDomainRedirectRepository(handle)
	custom := NewCustomDomainRepository(pool)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM domain_redirects WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, f.other})
		_, _ = pool.Exec(context.Background(), `DELETE FROM pool_link_instances WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, f.other})
	})
	domain := "serve-" + f.org.String()[:8] + ".io"

	// Cloud serves it: this instance neither answers for it nor asks for its certificate.
	cloudRow := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.org, Domain: domain, TargetURL: "https://acme.com", IncludeWWW: true,
		VerifyToken: "tok", ServedBy: models.RedirectServedByCloud}
	mustImport(t, redirects.Upsert(ctx, cloudRow, &f.owner))
	mustImport(t, redirects.SetCheck(ctx, cloudRow.ID, true, ""))
	records := []models.DNSRecord{{Purpose: "root", Type: "A", Name: domain, Value: "198.51.100.7"}}
	mustImport(t, redirects.SetRemote(ctx, cloudRow.ID, "t.warmbly.cloud", records))
	checked := time.Now()
	mustImport(t, redirects.SetReach(ctx, cloudRow.ID, &models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: models.RedirectHintNotRouted, Detail: "404", CheckedAt: &checked}))
	if _, ok, _ := redirects.Lookup(ctx, domain); ok {
		t.Fatal("this instance serves a redirect Cloud serves")
	}
	if ok, _ := custom.IsVerified(ctx, domain); ok {
		t.Fatal("this instance would get a certificate for a domain Cloud serves")
	}
	got, err := redirects.Get(ctx, f.org, domain)
	if err != nil || got.ServedBy != models.RedirectServedByCloud || got.RemoteHost != "t.warmbly.cloud" || len(got.RemoteRecords) != 1 ||
		got.Reach == nil || got.Reach.Hint != models.RedirectHintNotRouted || got.Reach.CheckedAt == nil {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	// A verdict from a newer Cloud this schema does not know is dropped, never a failed write.
	mustImport(t, redirects.SetReach(ctx, cloudRow.ID, &models.RedirectReach{Status: "brand_new", Hint: "brand_new"}))
	if got, _ = redirects.Get(ctx, f.org, domain); got.Reach != nil {
		t.Fatalf("an unknown status was stored: %+v", got.Reach)
	}
	mustImport(t, redirects.SetReach(ctx, cloudRow.ID, &models.RedirectReach{Status: models.RedirectReachOK, Hint: "brand_new"}))
	if got, _ = redirects.Get(ctx, f.org, domain); got.Reach == nil || got.Reach.Hint != "" {
		t.Fatalf("an unknown hint was stored: %+v", got.Reach)
	}
	// The database, not only the read before it, keeps a domain to one workspace's hands on Cloud.
	rival := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: domain, TargetURL: "https://evil.example", VerifyToken: "tok-r",
		ServedBy: models.RedirectServedByCloud}
	if err := redirects.Upsert(ctx, rival, &f.owner); !errors.Is(err, ErrRedirectTaken) {
		t.Fatalf("a second workspace handed the same domain to Cloud: %v", err)
	}
	if taken, _ := redirects.CloudServedElsewhere(ctx, f.other, domain); !taken {
		t.Fatal("another workspace was not told Cloud already serves the domain")
	}

	// Moving it here drops Cloud's records, reach and verdict; this instance proves the domain itself.
	here := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.org, Domain: domain, TargetURL: "https://acme.com", IncludeWWW: true,
		VerifyToken: "tok", ServedBy: models.RedirectServedByInstance}
	mustImport(t, redirects.Upsert(ctx, here, &f.owner))
	if here.RemoteHost != "" || here.RemoteRecords != nil || here.Reach != nil || here.Verified || here.VerifiedAt != nil {
		t.Fatalf("a move kept the old server's state: %+v", here)
	}
	if _, ok, _ := redirects.Lookup(ctx, domain); ok {
		t.Fatal("a redirect moved here is served on Cloud's verdict")
	}
	mustImport(t, redirects.SetCheck(ctx, here.ID, true, ""))
	if target, ok, _ := redirects.Lookup(ctx, domain); !ok || target != "https://acme.com" {
		t.Fatal("a verified redirect served here is not answered")
	}

	// A redirect visitors do not reach is checked again within minutes; one this server could not open is not.
	due := func() bool {
		list, err := redirects.Due(ctx, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range list {
			if d.ID == here.ID {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		status models.RedirectReachStatus
		due    bool
	}{{models.RedirectReachNotReaching, true}, {models.RedirectReachHTTPSError, true}, {models.RedirectReachUnreachable, false}, {models.RedirectReachOK, false}} {
		mustImport(t, redirects.SetCheck(ctx, here.ID, true, ""))
		mustImport(t, redirects.SetReach(ctx, here.ID, &models.RedirectReach{Status: c.status}))
		if _, err := pool.Exec(ctx, `UPDATE domain_redirects SET last_checked_at = now() - interval '20 minutes' WHERE id = $1`, here.ID); err != nil {
			t.Fatal(err)
		}
		if got := due(); got != c.due {
			t.Fatalf("reach %s: due = %v", c.status, got)
		}
	}

	if list, _ := redirects.CloudServedDomains(ctx); list[domain] != uuid.Nil {
		t.Fatal("a redirect served here is listed as Cloud's")
	}

	// The link ending stops every cloud-served row.
	mustImport(t, redirects.Upsert(ctx, cloudRow, &f.owner))
	_, bindErr := pool.Exec(ctx, `UPDATE domain_redirects SET cloud_link_instance_id = $2 WHERE id = $1`, cloudRow.ID, uuid.Nil)
	mustImport(t, bindErr)
	mustImport(t, redirects.UnverifyCloudServed(ctx, uuid.Nil, "unlinked"))
	if got, _ = redirects.Get(ctx, f.org, domain); got.Verified || got.LastError != "unlinked" {
		t.Fatalf("still live after the link ended: %+v", got)
	}

	// On Cloud: a linked instance's row is the link's, out of the workspace's list and delete, and ends with it.
	instID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO pool_link_instances (id, organization_id, name, token_hash) VALUES ($1, $2, 'test', $3)`,
		instID, f.other, "hash-"+instID.String()); err != nil {
		t.Fatal(err)
	}
	linkedDomain := "linked-" + f.org.String()[:8] + ".io"
	linked := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: linkedDomain, TargetURL: "https://frost.se",
		IncludeWWW: true, VerifyToken: "tok-l", LinkedInstanceID: &instID}
	mustImport(t, redirects.Upsert(ctx, linked, nil))
	if list, _ := redirects.List(ctx, f.other); len(list) != 0 {
		t.Fatalf("the workspace lists the link's row: %+v", list)
	}
	// Neither owner's write lands on the other's row, however the reads before it raced.
	grab := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: linkedDomain, TargetURL: "https://evil.example", VerifyToken: "tok-x"}
	if err := redirects.Upsert(ctx, grab, &f.owner); !errors.Is(err, ErrRedirectOwned) {
		t.Fatalf("the workspace rewrote the link's row: %v", err)
	}
	ownDomain := "own-" + linkedDomain
	mustImport(t, redirects.Upsert(ctx, &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: ownDomain, TargetURL: "https://a.example", VerifyToken: "tok-o"}, &f.owner))
	steal := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: ownDomain, TargetURL: "https://evil.example", VerifyToken: "tok-o", LinkedInstanceID: &instID}
	if _, err := redirects.UpsertLinked(ctx, steal, nil, 100); !errors.Is(err, ErrRedirectOwned) {
		t.Fatalf("the link rewrote the workspace's row: %v", err)
	}
	if ok, _ := redirects.Delete(ctx, f.other, linkedDomain); ok {
		t.Fatal("the workspace deleted the link's row")
	}
	if n, _ := redirects.CountLinked(ctx, instID); n != 1 {
		t.Fatalf("count = %d", n)
	}
	// The limit counts under the instance's lock: a new domain past it is refused, an existing one still updates.
	second := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: "two-" + linkedDomain, TargetURL: "https://frost.se",
		VerifyToken: "tok-2", LinkedInstanceID: &instID}
	if ok, err := redirects.UpsertLinked(ctx, second, nil, 1); err != nil || ok {
		t.Fatalf("a redirect past the limit was taken: %v %v", ok, err)
	}
	again := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: linkedDomain, TargetURL: "https://frost.se/new",
		IncludeWWW: true, VerifyToken: "tok-l", LinkedInstanceID: &instID}
	if ok, err := redirects.UpsertLinked(ctx, again, nil, 1); err != nil || !ok || again.TargetURL != "https://frost.se/new" {
		t.Fatalf("an existing redirect at the limit could not be updated: %v %v %+v", ok, err, again)
	}
	// Saves racing each other still stop at the limit.
	raceID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO pool_link_instances (id, organization_id, name, token_hash) VALUES ($1, $2, 'race', $3)`,
		raceID, f.other, "hash-"+raceID.String()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var taken atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := &models.DomainRedirect{ID: uuid.New(), OrganizationID: f.other, Domain: fmt.Sprintf("race%d-%s", i, linkedDomain),
				TargetURL: "https://frost.se", VerifyToken: "tok", LinkedInstanceID: &raceID}
			if ok, err := redirects.UpsertLinked(ctx, r, nil, 5); err == nil && ok {
				taken.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if n, _ := redirects.CountLinked(ctx, raceID); n != 5 || taken.Load() != 5 {
		t.Fatalf("racing saves took %d (%d stored) against a limit of 5", taken.Load(), n)
	}

	if err := NewPoolLinkRepository(pool, testSessionConnector(pool)).RevokeInstance(ctx, instID); err != nil {
		t.Fatal(err)
	}
	if got, _ := redirects.GetLinked(ctx, instID, linkedDomain); got != nil {
		t.Fatal("the redirect outlived the link it was served for")
	}
}

func TestLiveMailboxSourcesSigninConversions(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 357)
	f := newImportFixture(t, pool)
	grants := NewDomainGrantRepository(handle)
	enc, err := encrypt.NewEncrypter(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	emails := NewEmailRepostory(handle, enc)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mailbox_domain_grants WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, f.other})
	})
	signin := func(email string) uuid.UUID {
		id := f.mailbox(t, pool, f.org, f.owner, email)
		if _, err := pool.Exec(ctx, `UPDATE email_accounts SET provider = 'gmail', auth_method = 'oauth', last_id = 42 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at) VALUES ($1, 'a', 'r', now())`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	g := &models.DomainGrant{ID: uuid.New(), OrganizationID: f.org, Provider: models.GrantProviderGoogle, Tenant: "conv.io", Domains: []string{"conv.io"}}
	mustImport(t, grants.Upsert(ctx, g, f.owner))

	alex, me := signin("alex@conv.io"), signin("me-"+f.org.String()[:8]+"@gmail.com")
	list, xerr := emails.ListSigninRetiring(ctx, f.org)
	if xerr != nil || len(list) != 2 {
		t.Fatalf("retiring = %+v, %v", list, xerr)
	}
	if other, _ := emails.ListSigninRetiring(ctx, f.other); len(other) != 0 {
		t.Fatal("another workspace listed the mailboxes")
	}

	t.Run("a sign-in with full mail access is not retiring, and a vendor that can reconnect is named", func(t *testing.T) {
		suffix := f.org.String()[:8] + "@conv.io"
		full, modify, other := signin("full-"+suffix), signin("modify-"+suffix), signin("other-"+suffix)
		zap, forge := uuid.New(), uuid.New()
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{full, modify, other})
			_, _ = pool.Exec(context.Background(), `DELETE FROM mailbox_vendor_connections WHERE id = ANY($1)`, []uuid.UUID{zap, forge})
		})
		for _, c := range []struct {
			id     uuid.UUID
			vendor string
		}{{zap, "zapmail"}, {forge, "mailforge"}} {
			if _, err := pool.Exec(ctx, `INSERT INTO mailbox_vendor_connections (id, organization_id, vendor, credentials) VALUES ($1, $2, $3, 'sealed')`, c.id, f.org, c.vendor); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = pool.Exec(ctx, `UPDATE email_accounts_oauth SET granted_scopes = $2 WHERE email_account_id = $1`, full,
			[]string{models.GoogleFullMailScope, "https://www.googleapis.com/auth/gmail.settings.basic"})
		_, _ = pool.Exec(ctx, `UPDATE email_accounts_oauth SET granted_scopes = $2 WHERE email_account_id = $1`, modify,
			[]string{"https://www.googleapis.com/auth/gmail.modify"})
		_, _ = pool.Exec(ctx, `UPDATE email_accounts SET vendor_connection_id = $2, vendor_mailbox_id = 'v1' WHERE id = $1`, modify, zap)
		_, _ = pool.Exec(ctx, `UPDATE email_accounts SET vendor_connection_id = $2, vendor_mailbox_id = 'v2' WHERE id = $1`, other, forge)

		list, xerr := emails.ListSigninRetiring(ctx, f.org)
		if xerr != nil {
			t.Fatal(xerr)
		}
		got := map[uuid.UUID]models.MigrationMailbox{}
		for _, m := range list {
			got[m.ID] = m
		}
		if _, listed := got[full]; listed {
			t.Fatal("a mailbox with full mail access is still listed")
		}
		if m, listed := got[modify]; !listed || m.Vendor != "zapmail" || m.VendorConnectionID == nil || *m.VendorConnectionID != zap {
			t.Fatalf("gmail.modify only = %+v, listed %v", m, listed)
		}
		if m, listed := got[other]; !listed || m.Vendor != "" || m.VendorConnectionID != nil {
			t.Fatalf("a vendor that cannot sign in was named: %+v, listed %v", m, listed)
		}
		if _, listed := got[alex]; !listed {
			t.Fatal("a mailbox with no recorded scopes is not listed")
		}
	})

	t.Run("onto the grant, in place", func(t *testing.T) {
		if ok, _ := emails.ConvertToDelegated(ctx, f.other, alex, models.InboxProviderGoogle, g.ID, "alex@conv.io", "google_workspace"); ok {
			t.Fatal("another workspace converted the mailbox")
		}
		if ok, _ := emails.ConvertToDelegated(ctx, f.org, alex, models.InboxProviderOutlook, g.ID, "alex@conv.io", ""); ok {
			t.Fatal("a Google mailbox moved onto a Microsoft grant")
		}
		ok, xerr := emails.ConvertToDelegated(ctx, f.org, alex, models.InboxProviderGoogle, g.ID, "alex@conv.io", "google_workspace")
		if xerr != nil || !ok {
			t.Fatalf("convert = %v, %v", ok, xerr)
		}
		d, _ := emails.GetDelegation(ctx, alex)
		if d == nil || d.GrantID != g.ID || d.Subject != "alex@conv.io" {
			t.Fatalf("delegation = %+v", d)
		}
		var tokens int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM email_accounts_oauth WHERE email_account_id = $1`, alex).Scan(&tokens)
		if tokens != 0 {
			t.Fatal("the per-mailbox token was kept")
		}
		if ok, _ := emails.ConvertToDelegated(ctx, f.org, alex, models.InboxProviderGoogle, g.ID, "alex@conv.io", ""); ok {
			t.Fatal("a second conversion matched")
		}
	})

	t.Run("an Outlook move drops its Graph cursors", func(t *testing.T) {
		box := signin("ol-" + f.org.String()[:8] + "@conv.io")
		if _, err := pool.Exec(ctx, `UPDATE email_accounts SET provider = 'outlook' WHERE id = $1`, box); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO email_delta_links (user_id, email_id, folder, delta_link) VALUES ($1, $2, 'inbox', 'https://graph.microsoft.com/v1.0/me/x')`, f.owner, box); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO email_sync_state (email_id, user_id, backfill_status, backfill_cursor) VALUES ($1, $2, 'running', '{"inbox":"https://graph.microsoft.com/v1.0/me/next"}')`, box, f.owner); err != nil {
			t.Fatal(err)
		}
		mg := &models.DomainGrant{ID: uuid.New(), OrganizationID: f.org, Provider: models.GrantProviderMicrosoft, Tenant: "t-conv", Domains: []string{"conv.io"}}
		mustImport(t, grants.Upsert(ctx, mg, f.owner))
		if ok, xerr := emails.ConvertToDelegated(ctx, f.org, box, models.InboxProviderOutlook, mg.ID, "graph-user", "microsoft365"); xerr != nil || !ok {
			t.Fatalf("convert = %v, %v", ok, xerr)
		}
		var links int
		var cursor, status string
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM email_delta_links WHERE email_id = $1`, box).Scan(&links)
		_ = pool.QueryRow(ctx, `SELECT backfill_cursor::text, backfill_status FROM email_sync_state WHERE email_id = $1`, box).Scan(&cursor, &status)
		if links != 0 || cursor != "{}" || status != "running" {
			t.Fatalf("links %d, cursor %s, status %s", links, cursor, status)
		}
	})

	// An enrollment belongs to the workspace's Cloud link, kept for every later subtest.
	instance := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO cloud_link (cloud_url, instance_id, token, organization_id) VALUES ('https://cloud.test', $1, 'fixture', $2)`, instance, f.org); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cloud_link WHERE instance_id = $1`, instance)
	})

	t.Run("a mailbox managed elsewhere is never offered or moved", func(t *testing.T) {
		box := signin("managed-" + f.org.String()[:8] + "@conv.io")
		if _, err := pool.Exec(ctx, `INSERT INTO cloud_link_mailboxes (email_account_id, remote_id, managed, instance_id) VALUES ($1, $2, true, $3)`, box, uuid.New(), instance); err != nil {
			t.Fatal(err)
		}
		list, _ := emails.ListSigninRetiring(ctx, f.org)
		for _, m := range list {
			if m.ID == box {
				t.Fatal("a managed mailbox was offered for the move")
			}
		}
		if ok, _ := emails.ConvertToDelegated(ctx, f.org, box, models.InboxProviderGoogle, g.ID, "x", ""); ok {
			t.Fatal("a managed mailbox was moved onto the grant")
		}
		if ref, _ := emails.FindInOrganization(ctx, f.org, "managed-"+f.org.String()[:8]+"@conv.io"); ref == nil || !ref.Managed {
			t.Fatalf("ref = %+v", ref)
		}
	})

	t.Run("onto an app password, in place", func(t *testing.T) {
		creds := &models.SmtpImap{
			SMTP: &models.Service{Host: "smtp.gmail.com", Port: 587, Username: "me@gmail.com", Password: "abcdabcdabcdabcd"},
			IMAP: &models.Service{Host: "imap.gmail.com", Port: 993, Username: "me@gmail.com", Password: "abcdabcdabcdabcd"},
		}
		if ok, _ := emails.ConvertGoogleToAppPassword(ctx, f.other, me, creds, "gmail"); ok {
			t.Fatal("another workspace switched the mailbox")
		}
		ok, xerr := emails.ConvertGoogleToAppPassword(ctx, f.org, me, creds, "gmail")
		if xerr != nil || !ok {
			t.Fatalf("convert = %v, %v", ok, xerr)
		}
		// A mailbox on the grant switches too, and lets go of the grant.
		if ok, xerr := emails.ConvertGoogleToAppPassword(ctx, f.org, alex, creds, "google_workspace"); xerr != nil || !ok {
			t.Fatalf("delegated convert = %v, %v", ok, xerr)
		}
		var grant *uuid.UUID
		var subject string
		_ = pool.QueryRow(ctx, `SELECT domain_grant_id, delegated_subject FROM email_accounts WHERE id = $1`, alex).Scan(&grant, &subject)
		if grant != nil || subject != "" {
			t.Fatalf("delegated after = %v %q", grant, subject)
		}
		if ok, _ := emails.ConvertGoogleToAppPassword(ctx, f.org, me, creds, "gmail"); ok {
			t.Fatal("a mailbox already on an app password switched again")
		}
		var provider, auth, backfill string
		var lastID *int64
		_ = pool.QueryRow(ctx, `SELECT provider, auth_method, last_id FROM email_accounts WHERE id = $1`, me).Scan(&provider, &auth, &lastID)
		_ = pool.QueryRow(ctx, `SELECT backfill_status FROM email_sync_state WHERE email_id = $1`, me).Scan(&backfill)
		if provider != "smtp_imap" || auth != models.MailAuthAppPassword || lastID != nil || backfill != "complete" {
			t.Fatalf("after = %s %s %v %s", provider, auth, lastID, backfill)
		}
		got, err := emails.GetSMTPCredentials(ctx, me)
		if err != nil || got.IMAPHost != "imap.gmail.com" || got.SMTPPassword != "abcdabcdabcdabcd" {
			t.Fatalf("stored credentials = %+v, %v", got, err)
		}
		if list, _ := emails.ListSigninRetiring(ctx, f.org); len(list) != 0 {
			t.Fatalf("still retiring: %+v", list)
		}
	})

	t.Run("back onto its own Google sign-in, in place", func(t *testing.T) {
		exp := time.Now().Add(time.Hour)
		if ok, _ := emails.ConvertToGoogleOAuth(ctx, f.other, me, "a", "r", exp, "", nil); ok {
			t.Fatal("another workspace converted the mailbox")
		}
		if ok, _ := emails.ConvertToGoogleOAuth(ctx, f.org, me, "a", "", exp, "", nil); ok {
			t.Fatal("converted without a refresh token")
		}
		full := []string{models.GoogleFullMailScope}
		for _, id := range []uuid.UUID{me, alex} {
			ok, xerr := emails.ConvertToGoogleOAuth(ctx, f.org, id, "access-"+id.String(), "refresh-"+id.String(), exp, "", full)
			if xerr != nil || !ok {
				t.Fatalf("convert %s = %v, %v", id, ok, xerr)
			}
			var provider, auth, subject string
			var grant *uuid.UUID
			_ = pool.QueryRow(ctx, `SELECT provider, auth_method, domain_grant_id, delegated_subject FROM email_accounts WHERE id = $1`, id).Scan(&provider, &auth, &grant, &subject)
			if provider != "gmail" || auth != models.MailAuthOAuth || grant != nil || subject != "" {
				t.Fatalf("after = %s %s %v %q", provider, auth, grant, subject)
			}
			var smtp int
			_ = pool.QueryRow(ctx, `SELECT count(*) FROM email_accounts_smtp_imap WHERE email_account_id = $1`, id).Scan(&smtp)
			creds, xerr := emails.GetOAuthCredentials(ctx, id)
			if smtp != 0 || xerr != nil || creds == nil || creds.RefreshToken != "refresh-"+id.String() || !slices.Equal(creds.GrantedScopes, full) {
				t.Fatalf("stored = %d smtp rows, %+v, %v", smtp, creds, xerr)
			}
		}
		if ok, _ := emails.ConvertToGoogleOAuth(ctx, f.org, me, "a", "r", exp, "", nil); ok {
			t.Fatal("a mailbox already on its own sign-in was converted again")
		}
		if list, _ := emails.ListSigninRetiring(ctx, f.org); len(list) != 0 {
			t.Fatalf("a reconnected mailbox is still retiring: %+v", list)
		}
	})

	t.Run("a new consent records its scopes, a refresh keeps them, and a reauth replaces them", func(t *testing.T) {
		exp := time.Now().Add(time.Hour)
		read := func() []string {
			creds, xerr := emails.GetOAuthCredentials(ctx, me)
			if xerr != nil {
				t.Fatal(xerr)
			}
			return creds.GrantedScopes
		}
		if err := emails.RefreshBoxToken(ctx, me, "a2", "r2", exp); err != nil {
			t.Fatal(err)
		}
		if got := read(); !slices.Equal(got, []string{models.GoogleFullMailScope}) {
			t.Fatalf("after refresh = %v", got)
		}
		modify := []string{"https://www.googleapis.com/auth/gmail.modify"}
		if err := emails.ReplaceBoxToken(ctx, me, "a3", "r3", exp, modify, nil); err != nil {
			t.Fatal(err)
		}
		if got := read(); !slices.Equal(got, modify) {
			t.Fatalf("after reauth = %v", got)
		}
		if err := emails.ReplaceBoxToken(ctx, me, "a4", "r4", exp, nil, nil); err != nil {
			t.Fatal(err)
		}
		if got := read(); got != nil {
			t.Fatalf("a consent that named nothing kept %v", got)
		}
	})
}
