package api

import (
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

func TestPortalOriginAllowedOnlyForVerifiedPortals(t *testing.T) {
	config.SetPortalHosts(func(host string) bool { return host == "portal.agency.test" })
	t.Cleanup(func() { config.SetPortalHosts(nil) })
	if !portalOriginAllowed(nil, "https://portal.agency.test") {
		t.Fatal("verified portal origin refused")
	}
	for _, origin := range []string{"https://evil.test", "http://portal.agency.test", "https://portal.agency.test:444", "https://sub.portal.agency.test"} {
		if portalOriginAllowed(nil, origin) {
			t.Fatalf("%s allowed", origin)
		}
	}
}
