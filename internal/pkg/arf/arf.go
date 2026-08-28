// Package arf parses Abuse Reporting Format messages (RFC 5965), the feedback
// loop report a mailbox provider sends when a recipient presses "spam".
//
// A complaint is the strongest negative signal a sender gets, and unlike a
// bounce it never arrives synchronously: it comes back as mail. Like the dsn
// package this only PARSES; resolving and suppressing stay control-plane.
package arf

import (
	"regexp"
	"strings"
)

// Report is the extracted result of parsing a feedback report.
type Report struct {
	// IsComplaint is true only for an abuse-type report. Other feedback types
	// (fraud, not-spam, opt-out) are deliberately excluded: acting on a
	// "not-spam" report as if it were a complaint would be backwards.
	IsComplaint bool
	// OriginalMessageID is the Message-ID of the reported outbound message,
	// which is how the complaint resolves to a campaign send.
	OriginalMessageID string
	// ComplainedRecipient is the address the report names, when the provider
	// disclosed it. Advisory only: the caller suppresses the contact the
	// resolved send actually went to, never an address a report asserts.
	ComplainedRecipient string
	// UserAgent is the reporting provider, kept for the event record.
	UserAgent string
}

var (
	reFeedbackType = regexp.MustCompile(`(?im)^\s*Feedback-Type:\s*([a-z-]+)`)
	reMessageID    = regexp.MustCompile(`(?im)^\s*Message-ID:\s*<([^>\s]+)>`)
	// Original-Rcpt-To only. Original-Mail-From is the SENDER, and reading it
	// as the complainer would suppress the customer's own address.
	reOriginalRcpt = regexp.MustCompile(`(?im)^\s*Original-Rcpt-To:\s*<?([^\s<>]+@[^\s<>]+?)>?\s*$`)
	reUserAgent    = regexp.MustCompile(`(?im)^\s*User-Agent:\s*(.+)$`)
)

// reportMarkers identify a feedback report in the envelope.
var reportMarkers = []string{"message/feedback-report", "report-type=feedback-report"}

// senderMarkers are the addresses providers send feedback loops from.
var senderMarkers = []string{
	"abuse@", "feedback@", "fbl@", "scomp@", "staff@hotmail.com",
	"complaints@", "abusedesk@",
}

// Detect reports whether an inbound message looks like a feedback report, from
// the cheap envelope signals only. Callers gate the full Parse on this.
func Detect(from, subject, contentType string) bool {
	ct := strings.ToLower(contentType)
	for _, m := range reportMarkers {
		if strings.Contains(ct, m) {
			return true
		}
	}
	f := strings.ToLower(from)
	for _, m := range senderMarkers {
		if strings.Contains(f, m) {
			return true
		}
	}
	s := strings.ToLower(subject)
	// The subject providers use for the report itself.
	return strings.Contains(s, "abuse report") || strings.Contains(s, "complaint about message") ||
		strings.Contains(s, "email feedback report")
}

// Parse extracts complaint details from the report body. Safe on unrelated
// bodies: fields stay empty and IsComplaint stays false.
//
// The Message-ID taken is the LAST one in the body. An ARF message carries the
// reported mail's own headers in its third part, after the machine-readable
// part, so the last Message-ID is the reported message rather than the report.
func Parse(body string) Report {
	var r Report

	if m := reFeedbackType.FindStringSubmatch(body); m != nil {
		r.IsComplaint = strings.EqualFold(strings.TrimSpace(m[1]), "abuse")
	}
	if m := reOriginalRcpt.FindStringSubmatch(body); m != nil {
		r.ComplainedRecipient = strings.TrimSpace(m[1])
	}
	if m := reUserAgent.FindStringSubmatch(body); m != nil {
		r.UserAgent = strings.TrimSpace(m[1])
	}
	if all := reMessageID.FindAllStringSubmatch(body, -1); len(all) > 0 {
		r.OriginalMessageID = strings.TrimSpace(all[len(all)-1][1])
	}
	return r
}
