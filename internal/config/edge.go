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
