package jobs

import "strings"

// isMachineOpen reports whether an open event came from an automated fetcher
// rather than a human-rendered view. The edge already filters crawlers and
// security scanners outright; this classifies the gray zone we still WANT to
// count (it is real delivery signal) but must not present as a human open:
//
//   - Apple Mail Privacy Protection prefetches every pixel at delivery time
//     with a WebKit UA that ends at the engine token. A real Safari/Mail
//     render continues with "Version/... Safari/...", so the bare suffix is
//     the canonical MPP fingerprint.
//   - A missing UA is never a real mail client or browser.
//
// Gmail's image proxy is deliberately treated as HUMAN: it fetches at open
// time (not delivery), and it is the only open signal Gmail exposes.
func isMachineOpen(userAgent *string) bool {
	if userAgent == nil {
		return true
	}
	ua := strings.ToLower(strings.TrimSpace(*userAgent))
	if ua == "" {
		return true
	}
	return strings.HasSuffix(ua, "(khtml, like gecko)")
}
