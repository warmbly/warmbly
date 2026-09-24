package delegation

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestVendorGrantCoversOnlyTheVendorsDomains(t *testing.T) {
	s, _, store, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()

	g, xerr := s.GrantFromVendor(context.Background(), org, user, models.GrantProviderMicrosoft, "Contoso.com", "", []string{"contoso.com", "elsewhere.io"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if g.Tenant != "11111111-1111-1111-1111-111111111111" || strings.Join(g.Domains, ",") != "contoso.com" {
		t.Fatalf("grant = %+v, want the tenant's domains the vendor holds and nothing else", g)
	}

	users, xerr := s.Users(context.Background(), org, g.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, u := range users {
		if !strings.HasSuffix(u.Email, "@contoso.com") {
			t.Fatalf("directory listed %s outside the grant", u.Email)
		}
	}

	narrow := uuid.New()
	g, xerr = s.GrantFromVendor(context.Background(), narrow, user, models.GrantProviderMicrosoft, "contoso.com", "", []string{"contoso.com"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	store.grants[g.ID].Domains = []string{"contoso.onmicrosoft.com"}
	if users, _ := s.Users(context.Background(), narrow, g.ID); len(users) != 0 {
		t.Fatalf("directory listed %+v on a domain the grant does not cover", users)
	}

	if _, xerr := s.GrantFromVendor(context.Background(), uuid.New(), user, models.GrantProviderMicrosoft, "contoso.com", "", []string{"elsewhere.io"}); xerr == nil || xerr.Identifier != ErrIDNotCovered {
		t.Fatalf("a vendor account that does not hold the domain was granted it: %v", xerr)
	}
}

func TestVendorGoogleGrantNeedsTheAdminMailbox(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	if _, xerr := s.GrantFromVendor(context.Background(), uuid.New(), uuid.New(), models.GrantProviderGoogle, "acme.io", "", []string{"acme.io"}); xerr == nil || xerr.Identifier != ErrIDProof {
		t.Fatalf("a Google grant with no administrator = %v", xerr)
	}
	g, xerr := s.GrantFromVendor(context.Background(), uuid.New(), uuid.New(), models.GrantProviderGoogle, "acme.io", "admin@acme.io", []string{"acme.io"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if strings.Join(g.Domains, ",") != "acme.io" || g.AdminEmail != "admin@acme.io" {
		t.Fatalf("grant = %+v", g)
	}
}

func TestAppIdentityNamesWhatTheVendorAuthorizes(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	id, scopes, roles, ok := s.AppIdentity(models.GrantProviderMicrosoft)
	if !ok || id != "app-id" || strings.Join(roles, ",") != "Mail.ReadWrite,Mail.Send,User.Read.All" || len(scopes) == 0 {
		t.Fatalf("microsoft = %q %v %v %v", id, scopes, roles, ok)
	}
	id, scopes, roles, ok = s.AppIdentity(models.GrantProviderGoogle)
	if !ok || id != "123456789" || len(scopes) != 3 || roles != nil {
		t.Fatalf("google = %q %v %v %v", id, scopes, roles, ok)
	}
}
