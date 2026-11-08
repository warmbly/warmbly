package config

import "net/url"

// ReportURLOn is a shared client report's public page on host, or on the shared portal address when host is empty; "" when neither is known.
func ReportURLOn(host, token string) string {
	base := PortalOriginOn(host)
	if base == "" {
		return ""
	}
	return base + "/r/" + url.PathEscape(token)
}
