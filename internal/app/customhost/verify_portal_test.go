package customhost

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/trackdns"
)

// cnameTo answers a domain whose CNAME points at pointsAt, the way trackdns.Verify would.
func cnameTo(pointsAt string) Verifier {
	return func(_ context.Context, domain, target string) trackdns.Result {
		if target == pointsAt {
			return trackdns.Result{Domain: domain, Target: target, Verified: true, Code: trackdns.CodeVerified}
		}
		return trackdns.Result{Domain: domain, Target: target, Code: trackdns.CodeWrongTarget}
	}
}

func TestVerifyPortalRequiresThePortalTarget(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.warmbly.test")
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	if res := VerifyPortal(context.Background(), cnameTo("portal.warmbly.test"), "portal.agency.test"); !res.Verified {
		t.Fatalf("CNAME to the portal target refused: %+v", res)
	}
	if res := VerifyPortal(context.Background(), cnameTo("forms.warmbly.test"), "portal.agency.test"); res.Verified {
		t.Fatal("CNAME to the forms host verified a portal domain while PORTAL_DOMAIN is set")
	}
}

func TestVerifyPortalWithoutPortalDomainVerifiesNothing(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.warmbly.test")
	t.Setenv("PORTAL_DOMAIN", "")
	for _, pointsAt := range []string{"forms.warmbly.test", ""} {
		if res := VerifyPortal(context.Background(), cnameTo(pointsAt), "portal.agency.test"); res.Verified {
			t.Fatalf("a portal domain verified against %q with no portal service: %+v", pointsAt, res)
		}
	}
}
