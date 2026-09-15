package jobs

import (
	"strings"
	"time"

	"github.com/mileusna/useragent"
	"github.com/warmbly/warmbly/internal/repository"
)

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

// isInstant reports whether an engagement arrived so soon after the step was
// dispatched that no person could have read the email yet. Security gateways
// (Safe Links, Proofpoint, Mimecast) open the pixel and walk every link at
// delivery time with an ordinary browser UA, which is exactly what the UA
// rules cannot see.
//
// The anchor is dispatch to the worker, so `window` has to cover the SMTP
// handshake, the sending provider's queue and transit to the recipient before
// the arrival scan it is aimed at. It is operator-editable for that reason:
// how long that takes is a property of the deployment, not of the code.
//
// An unknown dispatch time never counts as instant. Neither does an event
// stamped BEFORE the dispatch, which means the two clocks disagree rather than
// that someone read the mail early: the timing rule abstains there and the
// event is left to the user agent and source-network rules, which is the only
// honest answer when the one input this rule has is known to be wrong.
func isInstant(sentAt *time.Time, at time.Time, window time.Duration) bool {
	if sentAt == nil {
		return false
	}
	since := at.Sub(*sentAt)
	return since >= 0 && since < window
}

// engagement is one open or click as the per-event rules see it: what the
// request said about itself, what the edge made of where it came from, and the
// two clocks the timing rule compares.
type engagement struct {
	userAgent *string
	// scanner is the label the tracking edge put on the source network, empty
	// when it recognised none.
	scanner *string
	// probable says that network ALSO carries people's own requests, so the
	// match corroborates the timing rule instead of replacing it. Browser
	// isolation is the case: Proofpoint and Mimecast render a clicked page in
	// their own cloud, with a person on the other end of it.
	probable bool
	sentAt   *time.Time
	at       time.Time
}

// isScannerSource reports whether the tracking edge recognised the request's
// source as a mail-filtering network. That verdict outranks the user agent:
// the whole point of the network rules is that a security gateway walks a
// message with an ordinary browser's user agent.
func isScannerSource(scanner *string) bool {
	return scanner != nil && strings.TrimSpace(*scanner) != ""
}

// certainScanner is a source that only ever filters mail, so the match is the
// whole verdict.
func (e engagement) certainScanner() bool {
	return isScannerSource(e.scanner) && !e.probable
}

// probableScanner is a source that is a scanner most of the time but can carry
// a person, so the match is worth a wider window and nothing more.
func (e engagement) probableScanner() bool {
	return isScannerSource(e.scanner) && e.probable
}

// classifyClick applies the per-event click rules (the burst rule needs the
// click log and lives in the consumer). It returns whether the click is
// automated and the reason recorded with it; an empty reason is a person.
//
// `window` is the machine window for a click; `probable` is the wider one a
// recognised-but-not-certain source is measured against. A probable source
// outside its window is left to the remaining rules, exactly as an
// unrecognised one would be, so naming a network can only ever catch more
// scans and never take a click that already counted as human.
func classifyClick(e engagement, window, probable time.Duration) (bool, string) {
	if e.certainScanner() {
		return true, repository.LinkClickReasonScanner
	}
	if e.userAgent == nil || strings.TrimSpace(*e.userAgent) == "" {
		return true, repository.LinkClickReasonPrefetch
	}
	if e.probableScanner() && isInstant(e.sentAt, e.at, probable) {
		return true, repository.LinkClickReasonScanner
	}
	if isInstant(e.sentAt, e.at, window) {
		return true, repository.LinkClickReasonInstant
	}
	return false, ""
}

// eventTime is when the tracking service saw the event, falling back to now
// when the stamp is missing or unreadable, so consumer lag never turns a
// delivery-time scan into a plausible human open.
func eventTime(stamp string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, stamp); err == nil {
		return t
	}
	return time.Now()
}

// classifyOpen applies the per-event open rules and names the one that
// caught it: scanner for a fetch from a known mail-filtering network,
// prefetch for a mail proxy or a fetch with no browser, instant for a fetch
// inside the machine window after dispatch. An empty reason is a person.
//
// The two windows work as they do for clicks: a source the edge recognised but
// could not settle is measured against the wider one and otherwise left to the
// remaining rules.
func classifyOpen(e engagement, window, probable time.Duration) (bool, string) {
	if e.certainScanner() {
		return true, repository.EmailOpenReasonScanner
	}
	if isMachineOpen(e.userAgent) {
		return true, repository.EmailOpenReasonPrefetch
	}
	if e.probableScanner() && isInstant(e.sentAt, e.at, probable) {
		return true, repository.EmailOpenReasonScanner
	}
	if isInstant(e.sentAt, e.at, window) {
		return true, repository.EmailOpenReasonInstant
	}
	return false, ""
}

// clientName names the mail client or image proxy behind a user agent when
// it says so; empty for a plain browser, which the parsed fields describe.
func clientName(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case ua == "":
		return ""
	case strings.Contains(ua, "googleimageproxy"):
		return "Gmail"
	case strings.Contains(ua, "yahoomailproxy"), strings.Contains(ua, "yahoo mail"):
		return "Yahoo Mail"
	case strings.Contains(ua, "outlook"), strings.Contains(ua, "microsoft office"):
		return "Outlook"
	case strings.Contains(ua, "thunderbird"):
		return "Thunderbird"
	case strings.Contains(ua, "superhuman"):
		return "Superhuman"
	case strings.Contains(ua, "protonmail"), strings.Contains(ua, "proton mail"):
		return "Proton Mail"
	case strings.Contains(ua, "hey.com"):
		return "HEY"
	case strings.HasSuffix(ua, "(khtml, like gecko)"):
		// Apple Mail Privacy Protection's prefetch fingerprint.
		return "Apple Mail"
	}
	return ""
}

// deviceType folds the parser's flags into desktop, mobile, tablet or unknown.
func deviceType(ua useragent.UserAgent) string {
	switch {
	case ua.Tablet:
		return "tablet"
	case ua.Mobile:
		return "mobile"
	case ua.Desktop:
		return "desktop"
	default:
		return "unknown"
	}
}
