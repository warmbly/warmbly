package signuprisk

import (
	"strings"
	"testing"
)

func TestScoreDisposableIsTheStrongestSignal(t *testing.T) {
	got := Score("someone@mailinator.com", "203.0.113.5")
	if !got.Disposable {
		t.Error("a known throwaway domain was not flagged")
	}
	if got.Score < weightDisposable {
		t.Errorf("score = %d, want at least %d", got.Score, weightDisposable)
	}
	if len(got.Reasons) == 0 {
		t.Error("a flagged signup produced no reason for review")
	}
}

// The failure that matters most: flagging ordinary customers. A business on
// Gmail is completely normal and must score clean.
func TestScoreOrdinarySignupsAreClean(t *testing.T) {
	for _, email := range []string{
		"ada@acme.com",
		"ada@gmail.com",
		"ada.lovelace@outlook.com",
		"sales@some-startup.io",
		"ada@sub.domain.co.uk",
	} {
		got := Score(email, "203.0.113.5")
		if got.Score != 0 {
			t.Errorf("Score(%q) = %d with %v, want 0", email, got.Score, got.Reasons)
		}
		if got.Disposable {
			t.Errorf("Score(%q) flagged a real provider as disposable", email)
		}
	}
}

func TestScoreWeakSignals(t *testing.T) {
	// A tagged address at a free provider is weak, not damning.
	tagged := Score("ada+signup7@gmail.com", "203.0.113.5")
	if tagged.Score == 0 {
		t.Error("a tagged free-provider address scored nothing")
	}
	if tagged.Score >= weightDisposable {
		t.Errorf("a tagged address scored %d, which should be well below a disposable domain", tagged.Score)
	}
	// The same tag on a company domain is just someone's mail routing.
	if own := Score("ada+signup@acme.com", "203.0.113.5"); own.Score != 0 {
		t.Errorf("a tagged address on a company domain scored %d, want 0", own.Score)
	}

	if malformed := Score("not-an-email", "203.0.113.5"); malformed.Score == 0 {
		t.Error("an address with no domain scored nothing")
	}
	if noIP := Score("ada@acme.com", ""); noIP.Score == 0 {
		t.Error("a signup with no source address scored nothing")
	}
}

func TestScoreIsCapped(t *testing.T) {
	if got := Score("a+b@mailinator.com", ""); got.Score > 100 {
		t.Errorf("score = %d, want it capped at 100", got.Score)
	}
}

// Normalize exists so one person cannot open several accounts that look
// unrelated. It must not merge genuinely different addresses.
func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ada.Lovelace+signup@gmail.com", "adalovelace@gmail.com"},
		{"ada.lovelace@googlemail.com", "adalovelace@gmail.com"},
		{"ADA@ACME.COM", "ada@acme.com"},
		{"ada+tag@acme.com", "ada@acme.com"},
		// Dots are only ignored by Google. Stripping them everywhere would
		// merge two different people at providers that treat them as distinct.
		{"ada.lovelace@acme.com", "ada.lovelace@acme.com"},
		{"first.last@outlook.com", "first.last@outlook.com"},
		{"garbage", "garbage"},
	}
	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPrivateIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.1", "::1", "169.254.1.1"} {
		if !PrivateIP(ip) {
			t.Errorf("PrivateIP(%q) = false, want true", ip)
		}
	}
	for _, ip := range []string{"203.0.113.5", "8.8.8.8", "", "nonsense"} {
		if PrivateIP(ip) {
			t.Errorf("PrivateIP(%q) = true, want false", ip)
		}
	}
}

// A self-hosted install signing up over a LAN is the normal case, so a private
// address must not add risk.
func TestPrivateAddressesAreNotPenalised(t *testing.T) {
	lan := Score("ada@acme.com", "192.168.1.50")
	public := Score("ada@acme.com", "203.0.113.5")
	if lan.Score != public.Score {
		t.Errorf("LAN signup scored %d vs %d for a public address", lan.Score, public.Score)
	}
}

func TestReasonsAreHumanReadable(t *testing.T) {
	got := Score("x@mailinator.com", "")
	for _, r := range got.Reasons {
		if strings.TrimSpace(r) == "" || strings.ToLower(r) != r && strings.ToUpper(r) == r {
			t.Errorf("reason %q is not a readable sentence", r)
		}
	}
}
