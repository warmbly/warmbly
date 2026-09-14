package replyclassify

import "strings"

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

	// --- Out-of-office signals (most specific machine reply) ---
	// Subject conventions providers emit for vacation autoresponders. Matched
	// on the SUBJECT only, never the body: the subject line of an
	// autoresponder is written by the mail provider, while "I'm on holiday
	// next week" in a body is a human reply, and reading that as automated
	// would stop stop_on_reply from firing for a person who actually answered.
	if matchesOOOSubject(subject) {
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

	// --- Bounce / delivery-status report signals => auto_reply (machine) ---
	// multipart/report; report-type=delivery-status is a DSN bounce.
	ct := strings.ToLower(h.first("Content-Type"))
	if strings.Contains(ct, "multipart/report") &&
		(strings.Contains(ct, "delivery-status") || strings.Contains(ct, "disposition-notification")) {
		return Result{Class: ClassAutoReply, Confidence: 0.95, Source: SourceHeader}, true
	}

	// Null Return-Path <> is the canonical bounce / non-reply-expecting envelope.
	if rp := strings.TrimSpace(h.first("Return-Path")); rp == "<>" || rp == "" && h.has("Return-Path") {
		return Result{Class: ClassAutoReply, Confidence: 0.85, Source: SourceHeader}, true
	}

	// mailer-daemon / postmaster style senders are machine bounce sources.
	if from := strings.ToLower(h.first("From")); from != "" {
		if strings.Contains(from, "mailer-daemon") ||
			strings.Contains(from, "postmaster@") ||
			strings.Contains(from, "no-reply@") ||
			strings.Contains(from, "noreply@") ||
			strings.Contains(from, "donotreply@") {
			return Result{Class: ClassAutoReply, Confidence: 0.8, Source: SourceHeader}, true
		}
	}

	return Result{}, false
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
var oooSubjectMarkers = []string{
	// English
	"out of office", "out of the office", "automatic reply", "automated reply",
	"autoreply", "auto-reply", "auto reply", "auto:", "away:", "on vacation:", "vacation reply",
	// German
	"abwesenheit", "abwesend", "automatische antwort", "autom. antwort",
	"ausser haus", "nicht im buero", "im urlaub:",
	// French
	"reponse automatique", "absence du bureau", "message d'absence",
	// Spanish / Portuguese
	"respuesta automatica", "ausencia de la oficina", "ausencia temporal",
	"resposta automatica", "fora do escritorio",
	// Italian
	"risposta automatica", "fuori sede:", "assente dall'ufficio",
	// Dutch
	"automatisch antwoord", "afwezigheid", "afwezigheidsbericht",
	// Nordic / Polish
	"automatiskt svar", "automatisk svar", "autosvar", "fravaer", "fravaersmelding",
	"automatyczna odpowiedz",
}

// matchesOOOSubject reports whether a subject was written by a vacation
// autoresponder. Accents are folded so each marker is listed in one spelling.
func matchesOOOSubject(subject string) bool {
	subject = foldAccents(subject)
	if subject == "" {
		return false
	}
	for _, m := range oooSubjectMarkers {
		if strings.HasPrefix(subject, m) {
			return true
		}
	}
	return false
}
