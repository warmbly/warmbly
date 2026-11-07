package email

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
)

func TestVendorConsentConnectsOnlyTheExpectedAddress(t *testing.T) {
	sess := &models.VendorConsentState{Email: "ava@acme.io"}
	if xerr := vendorConsentOwner(sess, &inboxOwner{Email: " Ava@ACME.io "}); xerr != nil {
		t.Fatalf("the expected address in another case was refused: %v", xerr)
	}
	for _, other := range []string{"leo@acme.io", "ava@acme.io.evil.test", "ava@acme.i", ""} {
		if xerr := vendorConsentOwner(sess, &inboxOwner{Email: other}); xerr != ErrVendorConsentWrongMailbox {
			t.Fatalf("owner %q = %v, want the wrong-mailbox refusal", other, xerr)
		}
	}
	if xerr := vendorConsentOwner(sess, nil); xerr != ErrVendorConsentWrongMailbox {
		t.Fatalf("no owner = %v", xerr)
	}
}

func TestVendorConsentRefusesAStateItDidNotMint(t *testing.T) {
	svc := &emailService{}
	for _, state := range []string{"", "w.abc", "abc", "vcx"} {
		if _, xerr := svc.FinishVendorConsent(context.Background(), "code", state, "", nil); xerr != ErrVendorConsentState {
			t.Fatalf("state %q = %v", state, xerr)
		}
	}
}

// consentRows is the import side of a vendor sign-in, recording how each ended.
type consentRows struct {
	wanted  bool
	settled map[string]string
}

func (r *consentRows) ResolveSignin(context.Context, uuid.UUID, string, uuid.UUID) error { return nil }
func (r *consentRows) VendorConsentWanted(context.Context, uuid.UUID, uuid.UUID, int, string) bool {
	return r.wanted
}
func (r *consentRows) VendorConsentSettled(_ context.Context, _, _ uuid.UUID, email, reason string) {
	r.settled[email] = reason
}

func vendorConsentFixture(t *testing.T) (*emailService, *cache.Cache) {
	t.Helper()
	redisURL := os.Getenv("WARMBLY_TEST_REDIS")
	if redisURL == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	t.Setenv("BOX_GOOGLE_CLIENT_ID", "client-1.apps.googleusercontent.com")
	t.Setenv("BOX_GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("BOX_GOOGLE_OAUTH_CONNECT", "")
	return &emailService{r: c, oauthInbox: &config.Oauth2Inbox{Google: config.GoogleOauth2Inbox("https://api.example.test")}}, c
}

func mintConsent(t *testing.T, svc *emailService) (string, models.VendorConsentRequest) {
	t.Helper()
	req := models.VendorConsentRequest{OrganizationID: uuid.New(), UserID: uuid.New(), ImportID: uuid.New(), Line: 4, ConnectionID: uuid.New(), Email: "ava@acme.io"}
	link, xerr := svc.VendorConsentURL(context.Background(), req)
	if xerr != nil {
		t.Fatal(xerr)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	state := q.Get("state")
	if !IsVendorConsentState(state) || q.Get("login_hint") != "ava@acme.io" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" {
		t.Fatalf("consent link = %s", link)
	}
	t.Cleanup(func() { _ = svc.r.Del(context.Background(), vendorConsentKey(state)).Err() })
	return state, req
}

func TestVendorConsentStateIsBoundSingleUseAndExpires(t *testing.T) {
	svc, c := vendorConsentFixture(t)
	ctx := context.Background()
	state, req := mintConsent(t, svc)

	if ttl := c.TTL(ctx, vendorConsentKey(state)).Val(); ttl < VendorConsentTTL-time.Minute || ttl > VendorConsentTTL {
		t.Fatalf("state ttl = %v", ttl)
	}
	// The dashboard's finish cannot read a vendor state.
	if _, xerr := svc.takeOnboardingState(ctx, state); xerr != errx.ErrEmailOnboardState {
		t.Fatalf("dashboard path read a vendor state: %v", xerr)
	}
	raw, _ := c.Get(ctx, vendorConsentKey(state)).Bytes()
	if strings.Contains(string(raw), "code_challenge") {
		t.Fatal("state holds the challenge instead of the verifier")
	}

	got, xerr := svc.takeVendorConsent(ctx, state)
	if xerr != nil || got.OrganizationID != req.OrganizationID || got.UserID != req.UserID || got.ImportID != req.ImportID ||
		got.Line != 4 || got.ConnectionID != req.ConnectionID || got.Email != "ava@acme.io" || got.CodeVerifier == "" {
		t.Fatalf("state = %+v, %v", got, xerr)
	}
	if _, xerr := svc.takeVendorConsent(ctx, state); xerr != ErrVendorConsentState {
		t.Fatalf("a replayed state = %v", xerr)
	}

	expired := VendorConsentStatePrefix + uuid.NewString()
	body, _ := json.Marshal(models.VendorConsentState{OrganizationID: uuid.New(), UserID: uuid.New(), Email: "ava@acme.io", CodeVerifier: "v", ExpiresAt: time.Now().Add(-time.Second)})
	if err := c.Set(ctx, vendorConsentKey(expired), body, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.takeVendorConsent(ctx, expired); xerr != ErrVendorConsentState {
		t.Fatalf("an expired state = %v", xerr)
	}
}

func TestVendorConsentChecksTheMemberBeforeTheCode(t *testing.T) {
	svc, _ := vendorConsentFixture(t)
	rows := &consentRows{wanted: true, settled: map[string]string{}}
	svc.importSignin = rows
	ctx := context.Background()

	state, req := mintConsent(t, svc)
	asked := ""
	_, xerr := svc.FinishVendorConsent(ctx, "code", state, "", func(userID string) FinishAuthorizer {
		asked = userID
		return func(context.Context, uuid.UUID, bool) *errx.Error { return errx.ErrForbidden }
	})
	if xerr != errx.ErrForbidden || asked != req.UserID.String() {
		t.Fatalf("a removed member = %v (asked for %q)", xerr, asked)
	}
	if rows.settled["ava@acme.io"] == "" {
		t.Fatal("the refusal did not reach the import")
	}

	state, _ = mintConsent(t, svc)
	allow := func(string) FinishAuthorizer {
		return func(context.Context, uuid.UUID, bool) *errx.Error { return nil }
	}
	if _, xerr := svc.FinishVendorConsent(ctx, "code", state, "access_denied", allow); xerr != ErrVendorConsentDeclined {
		t.Fatalf("a declined consent = %v", xerr)
	}

	rows.wanted = false
	state, _ = mintConsent(t, svc)
	if _, xerr := svc.FinishVendorConsent(ctx, "code", state, "", allow); xerr != ErrVendorConsentNotWanted {
		t.Fatalf("a row that no longer waits = %v", xerr)
	}
}
