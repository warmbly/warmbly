package replyclassify

import (
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/dsn"
)

// classifyHeaders is Layer 1: a deterministic, offline scan of the message
// headers (plus subject) for the well-known machine-reply markers. It returns
// (result, true) when it definitively recognizes an automated message; (zero,
// false) otherwise so the pipeline falls through to the lexicon/model layers.
//
// We map out_of_office vs auto_reply where the signal distinguishes them, and
// fold bounces / delivery-status reports into auto_reply (a non-human, machine
// reply). Bounces are NOT given a distinct class on purpose: campaign branching
// only needs "automated vs human", a hard bounce already drives suppression and
// the dedicated bounced_at signal elsewhere, and a separate "bounce" class would
// have no branch field to route on. This is documented here as the deliberate
// choice for the contract's "bounces => ... your call but document it" clause.
func classifyHeaders(in Input) (Result, bool) {
	h := newHeaderLookup(in.Headers)
	subject := strings.ToLower(strings.TrimSpace(in.Subject))

	// --- Delivery failures, before anything else ---
	// A bounce also carries Auto-Submitted: auto-replied, which on its own
	// reads as a vacation responder and would hold the lead as out of office.
	if isDeliveryFailure(h, subject, in.Languages) {
		return Result{Class: ClassAutoReply, Confidence: 0.95, Source: SourceHeader}, true
	}

	// --- Out-of-office signals (most specific machine reply) ---
	// Subject conventions providers emit for vacation autoresponders. Matched
	// on the SUBJECT only, never the body: the subject line of an
	// autoresponder is written by the mail provider, while "I'm on holiday
	// next week" in a body is a human reply, and reading that as automated
	// would stop stop_on_reply from firing for a person who actually answered.
	if matchesOOOSubject(subject, in.Languages) {
		return Result{Class: ClassOutOfOffice, Confidence: 0.98, Source: SourceHeader}, true
	}

	// RFC 3834 Auto-Submitted. "auto-replied" is canonically a vacation/auto
	// responder; "auto-generated" is any machine-generated message.
	if as := strings.ToLower(h.first("Auto-Submitted")); as != "" && as != "no" {
		if strings.Contains(as, "auto-replied") {
			return Result{Class: ClassOutOfOffice, Confidence: 0.95, Source: SourceHeader}, true
		}
		return Result{Class: ClassAutoReply, Confidence: 0.95, Source: SourceHeader}, true
	}

	// Vendor auto-responder headers (set by Exchange, Zimbra, helpdesks, etc.).
	if h.has("X-Autoreply") || h.has("X-Autorespond") ||
		strings.EqualFold(h.first("X-Autoreply"), "yes") {
		return Result{Class: ClassOutOfOffice, Confidence: 0.93, Source: SourceHeader}, true
	}
	if ars := h.first("X-Auto-Response-Suppress"); ars != "" {
		// Present on Exchange auto-responses; the message itself is automated.
		return Result{Class: ClassAutoReply, Confidence: 0.9, Source: SourceHeader}, true
	}

	// Precedence: bulk/junk/auto_reply marks list/automated traffic.
	if prec := strings.ToLower(h.first("Precedence")); prec != "" {
		switch prec {
		case "auto_reply":
			return Result{Class: ClassAutoReply, Confidence: 0.9, Source: SourceHeader}, true
		case "bulk", "junk", "list":
			return Result{Class: ClassAutoReply, Confidence: 0.75, Source: SourceHeader}, true
		}
	}

	// A read receipt is a machine report too.
	if ct := strings.ToLower(h.first("Content-Type")); strings.Contains(ct, "multipart/report") &&
		strings.Contains(ct, "disposition-notification") {
		return Result{Class: ClassAutoReply, Confidence: 0.95, Source: SourceHeader}, true
	}

	// Null Return-Path <> is the canonical bounce / non-reply-expecting envelope.
	if rp := strings.TrimSpace(h.first("Return-Path")); rp == "<>" || rp == "" && h.has("Return-Path") {
		return Result{Class: ClassAutoReply, Confidence: 0.85, Source: SourceHeader}, true
	}

	// No-reply senders are machines.
	if from := strings.ToLower(h.first("From")); from != "" {
		if strings.Contains(from, "no-reply@") ||
			strings.Contains(from, "noreply@") ||
			strings.Contains(from, "donotreply@") {
			return Result{Class: ClassAutoReply, Confidence: 0.8, Source: SourceHeader}, true
		}
	}

	return Result{}, false
}

// IsDeliveryFailure reports a bounce or delivery-status notice.
func IsDeliveryFailure(in Input) bool {
	return isDeliveryFailure(newHeaderLookup(in.Headers), strings.ToLower(strings.TrimSpace(in.Subject)), in.Languages)
}

// isDeliveryFailure reports a bounce from the signals a failure notice carries:
// a delivery-status report, the failed-recipients header Gmail and Exim add,
// a mail-system sender, or a mail server's own subject line.
func isDeliveryFailure(h headerLookup, subject string, langs []string) bool {
	ct := strings.ToLower(h.first("Content-Type"))
	if strings.Contains(ct, "multipart/report") && strings.Contains(ct, "delivery-status") {
		return true
	}
	if h.first("X-Failed-Recipients") != "" {
		return true
	}
	// The same sender and subject lists the worker's bounce parser reads.
	if dsn.IsBounceSender(h.first("From")) || dsn.HasBounceSubject(subject) {
		return true
	}
	// A workspace's own languages add the subjects their servers write.
	for _, m := range rulesFor(langs).bounce {
		if strings.HasPrefix(subject, m) {
			return true
		}
	}
	return false
}

// FlagHeaders reads the "Header:value" pseudo-flags the sync stores next to
// IMAP flags back into a header map. System flags ("\Seen") are skipped.
func FlagHeaders(flags []string) map[string][]string {
	h := map[string][]string{}
	for _, flag := range flags {
		i := strings.Index(flag, ":")
		if i <= 0 {
			continue
		}
		name := strings.TrimSpace(flag[:i])
		if name == "" || strings.HasPrefix(name, "\\") {
			continue
		}
		h[name] = append(h[name], strings.TrimSpace(flag[i+1:]))
	}
	return h
}

// IsBulkMail reports mail sent to a list, a newsletter or a notification:
// it carries List-Unsubscribe or List-Id, and its footer's "unsubscribe" is
// the sender's own, not a request from anyone to us.
func IsBulkMail(headers map[string][]string) bool {
	h := newHeaderLookup(headers)
	return h.first("List-Unsubscribe") != "" || h.first("List-Id") != ""
}

// headerLookup is a case-insensitive view over an email header map. MIME header
// maps are normally canonical-cased ("Auto-Submitted"), but inbound sync may
// hand us lower-cased keys, so we index both.
type headerLookup struct {
	byLower map[string][]string
}

func newHeaderLookup(h map[string][]string) headerLookup {
	m := make(map[string][]string, len(h))
	for k, v := range h {
		m[strings.ToLower(k)] = v
	}
	return headerLookup{byLower: m}
}

func (h headerLookup) first(name string) string {
	if vs := h.byLower[strings.ToLower(name)]; len(vs) > 0 {
		return strings.TrimSpace(vs[0])
	}
	return ""
}

func (h headerLookup) has(name string) bool {
	_, ok := h.byLower[strings.ToLower(name)]
	return ok
}

// oooSubjectMarkers are the subject conventions a mail provider writes on a
// vacation autoresponder, in the languages a European cold outreach list
// actually answers in. English-only markers were the reason a German
// "Automatische Antwort:" was only ever caught when it happened to carry
// Auto-Submitted or a vendor header (issue #470).
//
// Two rules keep a human reply out of the automated class, and both matter
// because an automated verdict stops replied_at being stamped, which is what
// makes stop_on_reply fire:
//
//   - matched as a PREFIX of the raw subject, never anywhere inside it. A
//     reply from a person is "Re: <our own campaign subject>", so a prefix
//     rule cannot fire on it; a Contains rule would fire on every reply to a
//     campaign whose subject happened to carry one of these words.
//   - every entry is a string a provider writes, not one a person types. An
//     autoresponder answering us sends either its own marker followed by our
//     subject ("Automatic reply: <subject>") or its own text outright
//     ("Abwesenheitsnotiz"), and both are prefixes.
//
// Reply markers are deliberately NOT stripped first. "AW: Abwesenheitsnotiz"
// is a person forwarding or replying ABOUT an away message; the away message
// itself does not carry one.
// The markers themselves live in languages.go, the base set for everyone and
// more for each language a workspace reads mail in.

// matchesOOOSubject reports whether a subject was written by a vacation
// autoresponder. Accents are folded so each marker is listed in one spelling.
func matchesOOOSubject(subject string, langs []string) bool {
	subject = foldAccents(subject)
	if subject == "" {
		return false
	}
	for _, m := range rulesFor(langs).ooo {
		if strings.HasPrefix(subject, m) {
			return true
		}
	}
	return false
}
