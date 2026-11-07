// Package dsn parses delivery-status notifications (bounce messages, RFC 3464)
// well enough to decide whether a permanent failure occurred, who it was for,
// and which original message it concerns. It is deliberately conservative:
// callers act (suppress a recipient) only on a confirmed permanent failure, so
// a soft/transient bounce is never treated as hard.
package dsn

import (
	"regexp"
	"strings"
	"unicode"
)

// Report is the extracted result of parsing a bounce message.
type Report struct {
	// IsBounce is true when the message looks like a delivery-status report at
	// all (worth acting on); false for ordinary mail.
	IsBounce bool
	// Permanent is true only when a 5.x.x status or an explicit permanent
	// "Action: failed" was found. Transient (4.x.x / delayed) stays false.
	Permanent bool
	// FailedRecipient is the address that bounced (Final/Original-Recipient).
	FailedRecipient string
	// OriginalMessageID is the Message-ID of the original outbound message the
	// bounce concerns, recovered from the returned headers section.
	OriginalMessageID string
	// Status is the first recipient's enhanced status code, e.g. "5.2.2".
	Status string
	// Action is the first recipient's Action field, lowercased.
	Action string
	// Diagnostic is the remote server's reply from Diagnostic-Code without its
	// type prefix, whitespace collapsed and bounded by ReasonLimit.
	Diagnostic string
}

// ReasonLimit bounds Report.Reason in runes; a notice can paste a whole transcript.
const ReasonLimit = 500

var (
	reStatus     = regexp.MustCompile(`(?im)^\s*Status:\s*([245])\.\d{1,3}\.\d{1,3}`)
	reStatusCode = regexp.MustCompile(`(?im)^\s*Status:\s*([245]\.\d{1,3}\.\d{1,3})`)
	reDiagField  = regexp.MustCompile(`(?im)^[ \t]*Diagnostic-Code:[^;\r\n]*;[ \t]*(.*)$`)
	reFieldName  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*:`)
	reAction     = regexp.MustCompile(`(?im)^\s*Action:\s*(failed|delayed|delivered|relayed|expanded)`)
	reFinalRcpt  = regexp.MustCompile(`(?im)^\s*(?:Final|Original)-Recipient:\s*[^;\r\n]*;\s*<?([^\s<>]+@[^\s<>]+?)>?\s*$`)
	reMessageID  = regexp.MustCompile(`(?im)^\s*Message-ID:\s*<([^>\s]+)>`)
	reDiagnostic = regexp.MustCompile(`(?im)^\s*Diagnostic-Code:\s*[^;\r\n]*;\s*([245])\d\d`)
)

// senderMarkers identify a machine bounce source in the From line.
var senderMarkers = []string{"mailer-daemon", "postmaster@", "mail-daemon"}

// subjectMarkers are the subjects mail servers write on a failure notice.
var subjectMarkers = []string{
	"undeliverable", "undelivered mail", "delivery status notification",
	"returned mail", "delivery failure", "mail delivery failed", "mail delivery failure",
	"delivery has failed", "failure notice", "message not delivered", "delivery incomplete",
	"non remis", "unzustellbar",
}

// IsBounceSender reports a mail-system sender in a From line.
func IsBounceSender(from string) bool {
	f := strings.ToLower(from)
	for _, m := range senderMarkers {
		if strings.Contains(f, m) {
			return true
		}
	}
	return false
}

// HasBounceSubject reports a subject that starts as a mail server's failure
// notice does. A prefix, so a person's "Re: <subject>" never matches.
func HasBounceSubject(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	for _, m := range subjectMarkers {
		if strings.HasPrefix(s, m) {
			return true
		}
	}
	return false
}

// Detect reports whether an inbound message looks like a bounce, using only the
// cheap envelope signals (no body parse). Callers gate the full Parse on this.
func Detect(from, subject, contentType string) bool {
	if IsBounceSender(from) {
		return true
	}
	if strings.Contains(strings.ToLower(contentType), "multipart/report") {
		return true
	}
	s := strings.ToLower(subject)
	for _, m := range subjectMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// Parse extracts bounce details from the message body (the DSN parts). It is
// safe on non-DSN bodies: fields simply stay empty. Permanence is decided from
// the machine-readable Status / Diagnostic-Code / Action fields, never from the
// human-readable prose, so a "delayed" notice is never a permanent bounce.
func Parse(body string) Report {
	var r Report

	if m := reStatus.FindStringSubmatch(body); m != nil {
		r.IsBounce = true
		r.Permanent = m[1] == "5"
	}
	if !r.Permanent {
		if m := reDiagnostic.FindStringSubmatch(body); m != nil {
			r.IsBounce = true
			r.Permanent = m[1] == "5"
		}
	}
	if a := reAction.FindStringSubmatch(body); a != nil {
		r.IsBounce = true
		if strings.EqualFold(a[1], "failed") && r.hasNo4xx(body) {
			// "failed" is permanent per RFC 3464, but a co-present 4.x.x status
			// (retry-then-fail) means transient — defer to the status code.
			r.Permanent = true
		}
	}

	if m := reFinalRcpt.FindStringSubmatch(body); m != nil {
		r.FailedRecipient = strings.TrimSpace(m[1])
	}
	// The last Message-ID in the body belongs to the returned original message
	// (the DSN's own id, if present, is a header, not in the body parts).
	if ids := reMessageID.FindAllStringSubmatch(body, -1); len(ids) > 0 {
		r.OriginalMessageID = strings.TrimSpace(ids[len(ids)-1][1])
	}
	// Recipient, status and diagnostic come from one per-recipient group.
	group := failedGroup(body)
	if m := reFinalRcpt.FindStringSubmatch(group); m != nil {
		r.FailedRecipient = strings.TrimSpace(m[1])
	}
	if a := reAction.FindStringSubmatch(group); a != nil {
		r.Action = strings.ToLower(a[1])
	}
	if m := reStatusCode.FindStringSubmatch(group); m != nil {
		r.Status = m[1]
	}
	r.Diagnostic = diagnosticText(group)

	return r
}

var reBlankLine = regexp.MustCompile(`\r?\n[ \t]*\r?\n`)

// failedGroup is the per-recipient field group a notice reports: the first
// with a permanent status, else the first with a status or diagnostic.
func failedGroup(body string) string {
	first := ""
	for _, g := range reBlankLine.Split(body, -1) {
		if m := reStatusCode.FindStringSubmatch(g); m != nil && m[1][0] == '5' {
			return g
		}
		if first == "" && (reStatusCode.MatchString(g) || reDiagField.MatchString(g)) {
			first = g
		}
	}
	return first
}

// diagnosticText reads the first Diagnostic-Code value with its folded
// continuation lines.
func diagnosticText(body string) string {
	loc := reDiagField.FindStringSubmatchIndex(body)
	if loc == nil {
		return ""
	}
	parts := []string{body[loc[2]:loc[3]]}
	for _, line := range strings.Split(body[loc[1]:], "\n")[1:] {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || (line[0] != ' ' && line[0] != '\t') || reFieldName.MatchString(trimmed) {
			break
		}
		parts = append(parts, trimmed)
	}
	return bound(strings.Join(parts, " "))
}

// bound collapses whitespace, drops control characters and caps the length.
func bound(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(s); len(r) > ReasonLimit {
		s = string(r[:ReasonLimit])
	}
	return s
}

// Reason is the notice's own account of the failure, its status code and the
// remote server's reply, or fallback when the notice carries neither. The
// Status field leads unless the reply's own deciding code is the same, so it
// is the code StatusIn reads.
func (r Report) Reason(fallback string) string {
	switch {
	case r.Diagnostic != "" && (r.Status == "" || sameStatus(r.Diagnostic, r.Status)):
		return r.Diagnostic
	case r.Diagnostic != "":
		return bound(r.Status + " " + r.Diagnostic)
	case r.Status != "":
		return r.Status
	}
	return bound(fallback)
}

func sameStatus(text, status string) bool {
	got, ok := StatusIn(text)
	want, wok := parseStatus(status)
	return ok && wok && got == want
}

func (r Report) hasNo4xx(body string) bool {
	if m := reStatus.FindStringSubmatch(body); m != nil {
		return m[1] != "4"
	}
	return true
}

// reReturned starts the copy of the original message a notice returns, whose
// words are the sender's and say nothing about the delivery.
var reReturned = regexp.MustCompile(`(?i)(-{2,}\s*original message|original message headers|message/rfc822)`)

// noticeText is the mail system's own part of a notice.
func noticeText(body string) string {
	if loc := reReturned.FindStringIndex(body); loc != nil {
		return body[:loc[0]]
	}
	return body
}

var reTransient = regexp.MustCompile(`(?i)\b(delay|delayed|will retry|will be retried|temporar(y|ily)|still trying)\b`)

// IsTransientNotice reports a notice that says delivery is still being
// retried: a 4.x.x status, or the mail system's own words about a delay.
func IsTransientNotice(subject, body string) bool {
	if m := reStatus.FindStringSubmatch(body); m != nil && m[1] == "4" {
		return true
	}
	return reTransient.MatchString(subject) || reTransient.MatchString(noticeText(body))
}

// WithFailedRecipients reads a failure notice that carries no RFC 3464 status
// part, only the X-Failed-Recipients header Gmail and Exim add (a Google Group
// that refuses the post, say). It is permanent unless something in the notice
// says delivery is still being retried; a report that already decided keeps
// its own answer.
func (r Report) WithFailedRecipients(header, subject, body string) Report {
	header = strings.TrimSpace(header)
	if header == "" || r.Permanent {
		return r
	}
	if IsTransientNotice(subject, body) {
		return r
	}
	r.IsBounce = true
	r.Permanent = true
	if r.FailedRecipient == "" {
		first := strings.TrimSpace(strings.Split(header, ",")[0])
		r.FailedRecipient = strings.Trim(first, "<>")
	}
	if r.Diagnostic == "" {
		r.Diagnostic = proseDiagnostic(body)
	}
	return r
}

// reReplyLine is a line of the notice that quotes the remote server's reply;
// reInlineReply is Exim's older form, the reply after the host on one line.
var (
	reReplyLine   = regexp.MustCompile(`(?m)^[ \t]*[45]\d\d[ -]\S.*$`)
	reInlineReply = regexp.MustCompile(`(?i)remote (?:mail )?server[^\n]*?:\s+([45]\d\d[ -]\S[^\n]*)`)
)

// proseDiagnostic is the remote server's reply as a notice without a status
// part quotes it (Exim's "SMTP error from remote mail server", Gmail's "The
// response from the remote server was:"), with its continuation lines.
func proseDiagnostic(body string) string {
	notice := noticeText(body)
	loc := reReplyLine.FindStringIndex(notice)
	if loc == nil {
		if m := reInlineReply.FindStringSubmatch(notice); m != nil {
			return bound(m[1])
		}
		return ""
	}
	parts := []string{notice[loc[0]:loc[1]]}
	for _, line := range strings.Split(notice[loc[1]:], "\n")[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !reReplyLine.MatchString(line) && (line[0] != ' ' && line[0] != '\t') {
			break
		}
		parts = append(parts, trimmed)
	}
	return bound(strings.Join(parts, " "))
}
