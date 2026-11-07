package delegation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// assertionScope is the scope claim of a service-account JWT assertion.
func assertionScope(assertion string) string {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.Scope
}

func googleDelegatedMailbox(t *testing.T) (*Service, *fakeProviders, *fakeStore) {
	t.Helper()
	s, p, _, _, st := newTestService(t)
	org := uuid.New()
	g := googleGrant(t, s, p, org, uuid.New())
	st.delegation = &models.DelegatedMailbox{AccountID: uuid.New(), OrganizationID: org, Provider: models.InboxProviderGoogle,
		GrantID: g.ID, Subject: "alex" + "@acme.io", Email: "alex" + "@acme.io"}
	return s, p, st
}

// The IMAP token is minted alone under the optional scope; the API token keeps the exact list administrators authorized.
func TestDelegatedIMAPTokenAsksOnlyForFullMailAccess(t *testing.T) {
	s, p, st := googleDelegatedMailbox(t)
	if _, handled, xerr := s.IMAPAccessToken(context.Background(), st.delegation.AccountID); !handled || xerr != nil {
		t.Fatalf("handled=%v err=%v", handled, xerr)
	}
	if got := assertionScope(p.subjects[len(p.subjects)-1]); got != "https://mail.google.com/" {
		t.Fatalf("IMAP assertion scope = %q", got)
	}
	if _, _, xerr := s.AccessToken(context.Background(), st.delegation.AccountID); xerr != nil {
		t.Fatal(xerr)
	}
	want := strings.Join(config.GoogleDelegationScopes[:2], " ")
	if got := assertionScope(p.subjects[len(p.subjects)-1]); got != want {
		t.Fatalf("API assertion scope = %q, want %q", got, want)
	}
}

// An entry without the optional scope refuses the IMAP token and still serves the API one.
func TestDelegatedIMAPRefusalLeavesTheAPITokenAlone(t *testing.T) {
	s, p, st := googleDelegatedMailbox(t)
	p.googleRefuseScope = "https://mail.google.com/"
	_, handled, xerr := s.IMAPAccessToken(context.Background(), st.delegation.AccountID)
	if !handled || xerr == nil || xerr.Code != errx.Forbidden {
		t.Fatalf("IMAP token: handled=%v err=%v, want a 403 refusal", handled, xerr)
	}
	if tok, _, xerr := s.AccessToken(context.Background(), st.delegation.AccountID); xerr != nil || tok.AccessToken == "" {
		t.Fatalf("API token after an IMAP refusal: %+v %v", tok, xerr)
	}
}

func TestDelegatedIMAPTokenIsGoogleOnly(t *testing.T) {
	s, p, _, _, st := newTestService(t)
	org := uuid.New()
	g := microsoftGrant(t, s, p, org)
	st.delegation = &models.DelegatedMailbox{AccountID: uuid.New(), OrganizationID: org, Provider: models.InboxProviderOutlook, GrantID: g.ID, Subject: "u-1", Email: "alex" + "@contoso.com"}
	if _, handled, xerr := s.IMAPAccessToken(context.Background(), st.delegation.AccountID); !handled || xerr == nil {
		t.Fatalf("a Microsoft mailbox drew a Gmail IMAP token: handled=%v err=%v", handled, xerr)
	}
}
