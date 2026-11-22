package partner

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestGeneratedKeyCarriesItsModeAndIsStoredHashed(t *testing.T) {
	for _, mode := range []models.PartnerMode{models.PartnerModeLive, models.PartnerModeTest} {
		raw, prefix, suffix, hash, err := GenerateKey(mode)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := KeyMode(raw)
		if !ok || got != mode {
			t.Fatalf("%s key reads as mode %q (%v)", mode, got, ok)
		}
		if !strings.HasPrefix(raw, "wmbp_"+string(mode)+"_") || !strings.HasPrefix(raw, prefix) || !strings.HasSuffix(raw, suffix) {
			t.Fatalf("display parts %q/%q do not frame %q", prefix, suffix, raw)
		}
		if hash != HashKey(raw) || strings.Contains(hash, raw) || len(hash) != 64 {
			t.Fatalf("stored hash %q is not the SHA-256 of the key", hash)
		}
		if again, _, _, _, _ := GenerateKey(mode); again == raw {
			t.Fatal("two keys came out the same")
		}
	}
	for _, raw := range []string{"", "wmbly_abc", "wmbp_prod_abc", "Bearer wmbp_live_x"} {
		if _, ok := KeyMode(raw); ok {
			t.Fatalf("%q was read as a partner key", raw)
		}
	}
}

func TestReturnToStaysInsideTheAppOrOnAnAllowedHost(t *testing.T) {
	hosts := []string{"app.graphkit.com"}
	for raw, want := range map[string]bool{
		"":                                    true,
		"/app/emails":                         true,
		"/app/campaigns?tab=x":                true,
		"https://app.graphkit.com/w/1":        true,
		"https://APP.graphkit.com/w/1":        true,
		"//evil.example/x":                    false,
		"/\\evil.example":                     false,
		"https://evil.example/app":            false,
		"https://app.graphkit.com.evil.io/":   false,
		"https://user@app.graphkit.com/":      false,
		"javascript:alert(1)":                 false,
		"app/emails":                          false,
		"ftp://app.graphkit.com/":             false,
		"https://app.graphkit.com/\r\nx: y":   false,
		"https://app.graphkit.com:8443/w/1":   true,
		"http://localhost/app" + "?x=1":       false,
		"https:///app.graphkit.com/no-host/x": false,
	} {
		if _, ok := CleanReturnTo(raw, hosts); ok != want {
			t.Errorf("CleanReturnTo(%q) = %v, want %v", raw, ok, want)
		}
	}
}

func TestPartnerKeysCannotMintKeys(t *testing.T) {
	if _, _, xerr := parsePermissions([]string{"READ_CAMPAIGNS", "API_KEYS"}); xerr == nil {
		t.Fatal("a partner-minted key was allowed API_KEYS")
	}
	mask, names, xerr := parsePermissions([]string{"read_campaigns", "WEBHOOKS"})
	if xerr != nil || mask != models.APIPermReadCampaigns|models.APIPermWebhooks || len(names) != 2 {
		t.Fatalf("permissions = %d %v %v", mask, names, xerr)
	}
	if _, _, xerr := parsePermissions([]string{"NOPE"}); xerr == nil {
		t.Fatal("an unknown permission was accepted")
	}
}
