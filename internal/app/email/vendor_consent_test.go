package email

import (
	"context"
	"testing"

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
