package config

import (
	"os"
	"strings"
)

// Registering customer hostnames at an edge provider is OPTIONAL and off by default.
//
// It is for an instance whose tracking host sits on a Cloudflare for SaaS zone:
// Cloudflare refuses a customer name pointed at that host (errors 1000 and 1014)
// until the name is one of the zone's custom hostnames. Without both values the
// instance registers nothing and every record it shows is unchanged.
//
// CLOUDFLARE_SAAS_ZONE_ID    the zone the tracking host is on.
// CLOUDFLARE_SAAS_API_TOKEN  a token allowed to edit that zone's custom hostnames.
// CLOUDFLARE_SAAS_ORIGIN     optional origin for the hostnames it creates; empty uses the zone's fallback origin.
func CloudflareSaaSZoneID() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_ZONE_ID"))
}

func CloudflareSaaSAPIToken() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_API_TOKEN"))
}

func CloudflareSaaSOrigin() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_ORIGIN"))
}

// The portal edge zone is the same feature for agency portal domains, also OPTIONAL and off by default.
//
// Agency portal domains CNAME to PORTAL_DOMAIN, whose zone is usually not the tracking
// host's, so they get a zone of their own, where the portal service serves them. Without
// both values no portal domain is registered anywhere, exactly as before.
//
// CLOUDFLARE_SAAS_PORTAL_ZONE_ID    the zone PORTAL_DOMAIN is on.
// CLOUDFLARE_SAAS_PORTAL_API_TOKEN  a token allowed to edit that zone's custom hostnames.
// CLOUDFLARE_SAAS_PORTAL_ORIGIN     optional origin for the hostnames it creates; empty uses the zone's fallback origin.
// CLOUDFLARE_SAAS_PORTAL_WORKER     optional name of the portal Worker; each hostname then gets its own route to it,
//
//	for a zone that also holds other customer hostnames and so cannot route every one to the portal.
func CloudflareSaaSPortalZoneID() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_PORTAL_ZONE_ID"))
}

func CloudflareSaaSPortalAPIToken() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_PORTAL_API_TOKEN"))
}

func CloudflareSaaSPortalOrigin() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_PORTAL_ORIGIN"))
}

func CloudflareSaaSPortalWorker() string {
	return strings.TrimSpace(os.Getenv("CLOUDFLARE_SAAS_PORTAL_WORKER"))
}
