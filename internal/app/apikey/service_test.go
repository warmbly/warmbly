package apikey

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestGenerateKeyFormat(t *testing.T) {
	raw, prefix, suffix, hash, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey: %v", err)
	}

	if !strings.HasPrefix(raw, KeyPrefix) {
		t.Errorf("raw key %q missing prefix %q", raw, KeyPrefix)
	}
	// "wmbly_" (6) + 32 bytes base64url-no-pad (~43) = 49 chars.
	if got, want := len(raw), len(KeyPrefix)+43; got != want {
		t.Errorf("raw key length = %d, want %d", got, want)
	}
	if got, want := len(prefix), displayPrefixLen; got != want {
		t.Errorf("prefix length = %d, want %d", got, want)
	}
	if got, want := len(suffix), displaySuffixLen; got != want {
		t.Errorf("suffix length = %d, want %d", got, want)
	}
	if prefix != raw[:displayPrefixLen] {
		t.Errorf("prefix %q != raw[:%d]", prefix, displayPrefixLen)
	}
	if suffix != raw[len(raw)-displaySuffixLen:] {
		t.Errorf("suffix %q != raw[-%d:]", suffix, displaySuffixLen)
	}
	if got, want := len(hash), 64; got != want {
		t.Errorf("hex sha256 length = %d, want %d", got, want)
	}
	if hash != hashKey(raw) {
		t.Error("hash returned by generateKey doesn't match hashKey(raw)")
	}
}

func TestGenerateKeyUnique(t *testing.T) {
	// Sanity check that we're not returning the same key twice — a regression
	// here would be catastrophic. 100 keys is enough to catch a stuck PRNG.
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		raw, _, _, _, err := generateKey()
		if err != nil {
			t.Fatalf("generateKey: %v", err)
		}
		if _, dup := seen[raw]; dup {
			t.Fatalf("duplicate key generated: %s", raw)
		}
		seen[raw] = struct{}{}
	}
}

func TestValidateAllowedIPs(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		wantOK  bool
		wantOut []string
	}{
		{"empty", nil, true, []string{}},
		{"bare-ipv4", []string{"10.0.0.1"}, true, []string{"10.0.0.1"}},
		{"bare-ipv6", []string{"2001:db8::1"}, true, []string{"2001:db8::1"}},
		{"cidr-ipv4", []string{"192.168.0.0/16"}, true, []string{"192.168.0.0/16"}},
		{"cidr-ipv6", []string{"2001:db8::/32"}, true, []string{"2001:db8::/32"}},
		{"mixed", []string{"1.2.3.4", "10.0.0.0/8"}, true, []string{"1.2.3.4", "10.0.0.0/8"}},
		{"whitespace-trimmed", []string{"  10.0.0.1  "}, true, []string{"10.0.0.1"}},
		{"empty-string-skipped", []string{"10.0.0.1", ""}, true, []string{"10.0.0.1"}},
		{"bad-ip", []string{"not-an-ip"}, false, nil},
		{"bad-cidr", []string{"10.0.0.0/99"}, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAllowedIPs(tt.in)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(got) != len(tt.wantOut) {
					t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.wantOut), got)
				}
				for i, want := range tt.wantOut {
					if got[i] != want {
						t.Errorf("entry %d = %q, want %q", i, got[i], want)
					}
				}
			} else if err == nil {
				t.Fatalf("expected error, got %v", got)
			}
		})
	}
}

func TestValidateAllowedIPsCap(t *testing.T) {
	too_many := make([]string, maxAllowedIPs+1)
	for i := range too_many {
		too_many[i] = "10.0.0.1"
	}
	if _, err := validateAllowedIPs(too_many); err == nil {
		t.Fatal("expected error for too many entries")
	}
}

func TestValidateKeyIP(t *testing.T) {
	svc := &apiKeyService{}
	tests := []struct {
		name    string
		allowed []string
		ip      string
		want    bool
	}{
		{"no-restriction", nil, "1.2.3.4", true},
		{"exact-match", []string{"10.0.0.1"}, "10.0.0.1", true},
		{"exact-miss", []string{"10.0.0.1"}, "10.0.0.2", false},
		{"cidr-hit", []string{"10.0.0.0/8"}, "10.5.5.5", true},
		{"cidr-miss", []string{"10.0.0.0/8"}, "11.5.5.5", false},
		{"ipv6-cidr-hit", []string{"2001:db8::/32"}, "2001:db8::1", true},
		{"ipv6-cidr-miss", []string{"2001:db8::/32"}, "2002:db8::1", false},
		{"mixed-list-hit-second", []string{"1.2.3.4", "10.0.0.0/8"}, "10.0.0.99", true},
		{"malformed-client-ip", []string{"10.0.0.1"}, "not-an-ip", false},
		{"empty-client-ip-with-restriction", []string{"10.0.0.1"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &models.APIKey{AllowedIPs: tt.allowed}
			got := svc.ValidateKeyIP(k, tt.ip)
			if got != tt.want {
				t.Errorf("ValidateKeyIP(%v, %q) = %v, want %v", tt.allowed, tt.ip, got, tt.want)
			}
		})
	}
}

func TestValidateKeyPermission(t *testing.T) {
	svc := &apiKeyService{}
	k := &models.APIKey{Permissions: models.APIPermReadCampaigns | models.APIPermReadContacts}

	if !svc.ValidateKeyPermission(k, models.APIPermReadCampaigns) {
		t.Error("expected ReadCampaigns to be granted")
	}
	if svc.ValidateKeyPermission(k, models.APIPermWriteCampaigns) {
		t.Error("expected WriteCampaigns to be denied")
	}
}

func TestAllAPIPermissionsMask(t *testing.T) {
	// Sanity check: the mask must include every entry in AllAPIPermissions,
	// otherwise CreateAPIKey would reject a permission we expose in the UI.
	var union uint64
	for _, p := range models.AllAPIPermissions {
		union |= p.Value
	}
	if union != models.AllAPIPermissionsMask {
		t.Errorf("AllAPIPermissionsMask = %x, but union of AllAPIPermissions = %x", models.AllAPIPermissionsMask, union)
	}
}

func TestAPIPermPresetsAreSubset(t *testing.T) {
	if models.APIPermReadOnly&^models.AllAPIPermissionsMask != 0 {
		t.Error("APIPermReadOnly contains bits outside the master mask")
	}
	if models.APIPermFullAccess&^models.AllAPIPermissionsMask != 0 {
		t.Error("APIPermFullAccess contains bits outside the master mask")
	}
	if models.APIPermReadOnly&models.APIPermFullAccess != models.APIPermReadOnly {
		t.Error("APIPermFullAccess must include every bit in APIPermReadOnly")
	}
}
