package goog

import "testing"

// Gmail leaves verificationStatus off the primary address, which cannot be
// unverified. Reading that absence as "not verified" would hide the mailbox's
// own address from its own picker.
func TestSendAsIdentitiesPrimaryIsVerified(t *testing.T) {
	got := SendAsIdentities([]SendAs{
		{Email: "Sales@Acme.com", Name: "Acme Sales", IsPrimary: true, IsDefault: true},
		{Email: "hello@acme.com", VerificationStatus: "accepted"},
		{Email: "pending@acme.com", VerificationStatus: "pending"},
		{Email: "   "},
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

// The signature that travels is the one belonging to the address the mailbox
// actually sends as, because that is who the message signs off as.
func TestSignatureForFollowsTheChosenAlias(t *testing.T) {
	rows := []SendAs{
		{Email: "sales@acme.com", Signature: "<p>Sales</p>", IsPrimary: true, IsDefault: true},
		{Email: "hello@acme.com", Signature: "<p>Hello</p>", VerificationStatus: "accepted"},
	}

	if got := SignatureFor(rows, "HELLO@acme.com"); got != "<p>Hello</p>" {
		t.Errorf("picked the wrong identity's signature: %q", got)
	}
	// No alias chosen: the default identity signs.
	if got := SignatureFor(rows, ""); got != "<p>Sales</p>" {
		t.Errorf("default identity did not sign: %q", got)
	}
	// An alias the provider no longer lists falls back rather than returning
	// nothing, which would read as "the signature is empty".
	if got := SignatureFor(rows, "gone@acme.com"); got != "<p>Sales</p>" {
		t.Errorf("a missing alias did not fall back: %q", got)
	}
	if got := SignatureFor(nil, ""); got != "" {
		t.Errorf("no identities should yield no signature, got %q", got)
	}
}
