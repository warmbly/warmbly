package emailverify

import (
	"net"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/dsn"
)

// A 5xx reply to RCPT TO does NOT on its own mean the recipient is unknown.
// Servers use the same code range to reject the PROBE — a non-FQDN HELO, an
// unacceptable envelope sender, a blocklisted IP, relay policy. Postfix in
// particular defers its HELO/sender restrictions to RCPT time
// (smtpd_delay_reject is on by default), so a rejected HELO surfaces as
// "504 5.5.2 <localhost>: Helo command rejected: need fully-qualified
// hostname" *on the RCPT command*. Reading that as "this address is invalid"
// permanently drops a perfectly good contact, which is exactly what issue #200
// hit. Only a reply that talks about the RECIPIENT may produce StatusInvalid;
// everything else degrades to StatusUnknown, which still sends. The rule is
// dsn.Blame, shared with the bounce pipeline.

// classifyPermanentRcpt decides whether a permanent (5xx) RCPT rejection is
// about the recipient (a hard invalid we may record) or about the probe (which
// must degrade to unknown so a good contact is never dropped).
func classifyPermanentRcpt(code int, msg string) (probeOutcome, string) {
	rejected := "recipient rejected (" + strconv.Itoa(code) + "): " + msg
	unjudged := "probe rejected, address not judged (" + strconv.Itoa(code) + "): " + msg

	switch dsn.Blame(msg) {
	case dsn.CauseRecipientInvalid:
		return probeRejected, rejected
	case dsn.CauseUnknown:
		// Without an enhanced status only the codes that conventionally answer
		// an unknown mailbox count: 500-504 reject the COMMAND, 552 is a full
		// mailbox, 554 a catch-all "transaction failed".
		if _, ok := dsn.StatusIn(msg); !ok && (code == 550 || code == 551 || code == 553) {
			return probeRejected, rejected
		}
	}
	return probeUnknown, unjudged
}

// reservedHeloTLDs never resolve on the public internet, so announcing one is
// as unusable as announcing a bare hostname.
var reservedHeloTLDs = map[string]bool{
	"local": true, "localdomain": true, "localhost": true, "internal": true,
	"invalid": true, "test": true, "example": true, "home": true, "lan": true,
	"corp": true, "intranet": true, "private": true,
}

// isFQDN reports whether host is a public, fully-qualified hostname usable as
// an SMTP HELO identity. A bare name ("localhost"), an IP literal, or a
// reserved-TLD name is refused by RFC-compliant servers
// (Postfix's reject_non_fqdn_helo_hostname), which is what turns a probe into
// a false "invalid address" verdict.
func isFQDN(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h == "" || len(h) > 253 || strings.ContainsAny(h, " \t\r\n:/@[]_") {
		return false
	}
	if net.ParseIP(h) != nil {
		return false
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	if len(tld) < 2 || reservedHeloTLDs[tld] {
		return false
	}
	for i := 0; i < len(tld); i++ {
		if tld[i] < 'a' || tld[i] > 'z' {
			return false
		}
	}
	return true
}
