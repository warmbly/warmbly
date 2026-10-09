package oauth

import "testing"

func TestAppNameFollowsTheNamingRules(t *testing.T) {
	if got, err := appName("  Acme   Sync ", ""); err != nil || got != "Acme Sync" {
		t.Fatalf("appName = %q, %v", got, err)
	}
	for _, bad := range []string{"", "Verify at evil.example", "https://acme.com", "Acme ‮ppa", "<b>Acme</b>"} {
		if _, err := appName(bad, ""); err == nil {
			t.Errorf("appName(%q) accepted", bad)
		}
	}
}

func TestAppWebsite(t *testing.T) {
	for _, ok := range []string{"", "https://acme.com", "http://acme.com/path"} {
		if _, err := appWebsite(ok); err != nil {
			t.Errorf("appWebsite(%q) refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"javascript:alert(1)", "data:text/html,x", "acme.com", "https://user:pw@acme.com", "ftp://acme.com"} {
		if _, err := appWebsite(bad); err == nil {
			t.Errorf("appWebsite(%q) accepted", bad)
		}
	}
}

func TestAppNameRefusesThePlatformName(t *testing.T) {
	for _, bad := range []string{"Warmbly", "Warmbly Support", "HubSpot for warmbly", "W a r m b l y", "Warm-bly", "WarmbIy", "Warrnbly", "VVarmbly", "Wаrmbly", "Ｗａｒｍｂｌｙ"} {
		if _, err := appName(bad, ""); err == nil {
			t.Errorf("appName(%q) accepted", bad)
		}
	}
	for _, ok := range []string{"Acme Sync", "Warm Leads", "Swarm"} {
		if _, err := appName(ok, ""); err != nil {
			t.Errorf("appName(%q) refused: %v", ok, err)
		}
	}
	if _, err := appName("Warmbly Sync", "Warmbly Sync"); err != nil {
		t.Errorf("an unchanged existing name was refused: %v", err)
	}
	if _, err := appName("Warmbly Sync 2", "Warmbly Sync"); err == nil {
		t.Error("a changed name naming the platform was accepted")
	}
}

func TestDCRClientNameDropsThePlatformName(t *testing.T) {
	cases := map[string]string{
		"Claude Code (warmbly)":   "Claude Code",
		"Cursor [Warmbly prod]":   "Cursor",
		"Cursor":                  "Cursor",
		"Claude Code (work)":      "Claude Code (work)",
		"Warmbly Support":         dcrFallbackName,
		"Warmbly (Claude Code)":   dcrFallbackName,
		"":                        dcrFallbackName,
		"https://warmbly.example": dcrFallbackName,
	}
	for in, want := range cases {
		if got := dcrClientName(in); got != want {
			t.Errorf("dcrClientName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ImportedName("Warmbly Sync"); got != "Imported app" {
		t.Errorf("ImportedName kept the platform name: %q", got)
	}
}
