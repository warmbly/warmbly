package email

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

// Gmail leaves verificationStatus off the primary address, which cannot be
// unverified. Reading that absence as "not verified" would hide the mailbox's
// own address from its own picker.
func TestNormalizeSendAs_PrimaryIsVerified(t *testing.T) {
	got := normalizeSendAs([]gmailSendAs{
		{SendAsEmail: "Sales@Acme.com", DisplayName: "Acme Sales", IsPrimary: true, IsDefault: true},
		{SendAsEmail: "hello@acme.com", VerificationStatus: "accepted"},
		{SendAsEmail: "pending@acme.com", VerificationStatus: "pending"},
		{SendAsEmail: "   "},
	})

	if len(got) != 3 {
		t.Fatalf("expected the blank row to be dropped, got %d identities", len(got))
	}
	if got[0].Email != "sales@acme.com" {
		t.Errorf("address was not lowercased: %q", got[0].Email)
	}
	if !got[0].Verified || !got[1].Verified {
		t.Error("primary and accepted addresses must both be verified")
	}
	if got[2].Verified {
		t.Error("a pending address is not verified")
	}
}

// The signature imported is the one belonging to the address the mailbox
// actually sends as, because that is who the message signs off as.
func TestPickSignature_FollowsTheChosenAlias(t *testing.T) {
	rows := []gmailSendAs{
		{SendAsEmail: "sales@acme.com", Signature: "<p>Sales</p>", IsPrimary: true, IsDefault: true},
		{SendAsEmail: "hello@acme.com", Signature: "<p>Hello</p>", VerificationStatus: "accepted"},
	}

	sig, err := pickSignature(rows, "HELLO@acme.com")
	if err != nil || sig == nil {
		t.Fatalf("no signature picked: %v", err)
	}
	if sig.HTML != "<p>Hello</p>" {
		t.Errorf("picked the wrong identity's signature: %q", sig.HTML)
	}
	if sig.Plain != "Hello" {
		t.Errorf("plain text was not derived: %q", sig.Plain)
	}

	// No choice yet: the default identity signs.
	sig, err = pickSignature(rows, "")
	if err != nil || sig == nil || sig.HTML != "<p>Sales</p>" {
		t.Fatalf("default identity did not sign: %+v (%v)", sig, err)
	}
}

// An empty signature at the provider is an answer, not a failure. Storing it
// would delete a signature written here.
func TestPickSignature_EmptyKeepsWhatIsStored(t *testing.T) {
	sig, err := pickSignature([]gmailSendAs{{SendAsEmail: "a@b.com", IsPrimary: true, Signature: "  "}}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig != nil {
		t.Errorf("an empty provider signature must not overwrite: %+v", sig)
	}
}

func TestPickSignature_TooLargeIsRefused(t *testing.T) {
	huge := "<p>" + strings.Repeat("x", config.SignatureHTMLMax) + "</p>"
	if _, err := pickSignature([]gmailSendAs{{SendAsEmail: "a@b.com", IsPrimary: true, Signature: huge}}, ""); err == nil {
		t.Fatal("a signature past the stored maximum must be refused, not truncated")
	}
}
