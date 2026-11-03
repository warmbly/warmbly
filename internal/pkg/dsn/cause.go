package dsn

import (
	"strconv"
	"strings"
	"unicode"
)

// Cause names what a refusal was about. The identifiers are stored in event
// metadata, so changing one changes history.
type Cause string

const (
	// CauseUnknown means the text does not say, so callers keep their default.
	CauseUnknown          Cause = ""
	CauseRecipientInvalid Cause = "recipient_invalid"
	CauseMailboxFull      Cause = "mailbox_full"
	CauseReputationBlock  Cause = "reputation_block"
	CauseTransient        Cause = "transient"
)

// AddressIsFine reports a refusal that affirmatively names something other
// than the recipient's address: a full mailbox, a policy or reputation block,
// a system or network problem, or a temporary failure.
func (c Cause) AddressIsFine() bool {
	switch c {
	case CauseMailboxFull, CauseReputationBlock, CauseTransient:
		return true
	}
	return false
}

// mailboxFullMarkers say the mailbox exists and is over quota. They win over
// recipientMarkers because Postfix quota policies answer "Recipient address
// rejected: Mailbox full".
var mailboxFullMarkers = []string{
	"mailbox full",
	"mailbox is full",
	"over quota",
	"quota exceeded",
	"exceeded storage",
	"insufficient storage",
	"out of storage",
	"quotaexceeded",
}

// recipientMarkers name the RECIPIENT as the problem. They are decisive
// regardless of the numeric code, because providers disagree about which code
// carries them (Microsoft answers a missing mailbox with 5.4.1, some Exim
// builds with 554).
var recipientMarkers = []string{
	"user unknown",
	"unknown user",
	"no such user",
	"no such recipient",
	"no such mailbox",
	"user not found",
	"recipient not found",
	"recipient unknown",
	"unknown recipient",
	"recipient address rejected",
	"recipient rejected",
	"invalid recipient",
	"invalid mailbox",
	"mailbox unavailable",
	"mailbox not found",
	"mailbox does not exist",
	"mailbox is disabled",
	"mailbox disabled",
	"account has been disabled",
	"account is disabled",
	"account does not exist",
	"address does not exist",
	"does not exist",
	"doesn't exist",
	"unrouteable address",
	"unroutable address",
	"no mailbox here",
	"not a valid mailbox",
	"user doesn't have an account",
}

// transientMarkers say the sender should try again later.
var transientMarkers = []string{
	"greylist",
	"grey-list",
	"try again",
	"temporarily",
	"rate limit",
	"too many",
}

// sessionMarkers name the SENDER as the problem: its greeting, envelope
// sender, connecting IP, authentication or reputation, or a policy applied to
// the session. A refusal carrying one says nothing about the mailbox.
var sessionMarkers = []string{
	"helo",
	"ehlo",
	"fully-qualified",
	"fully qualified",
	"relay access denied",
	"relay denied",
	"relaying denied",
	"not permitted to relay",
	"unable to relay",
	"client host rejected",
	"client host blocked",
	"sender address rejected",
	"sender rejected",
	"sender verify failed",
	"sender not allowed",
	"authentication required",
	"auth required",
	"not authenticated",
	"not authorized",
	"blacklist",
	"blocklist",
	"blocked using",
	"listed in",
	"spamhaus",
	"reputation",
	"service unavailable",
	"connection refused",
	"policy reasons",
	"local policy",
	"security policy",
}

// Status is an RFC 3463 enhanced status code (class.subject.detail).
type Status struct {
	Class, Subject, Detail int
}

// StatusIn returns the most specific enhanced status code in text: the first
// that is not an undefined x.0.0, else the first. Tokens are whole, so an IP
// address or a version string never reads as one.
func StatusIn(text string) (Status, bool) {
	var first Status
	found := false
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' }) {
		s, ok := parseStatus(strings.Trim(tok, "."))
		if !ok {
			continue
		}
		if s.Subject != 0 || s.Detail != 0 {
			return s, true
		}
		if !found {
			first, found = s, true
		}
	}
	return first, found
}

func parseStatus(tok string) (Status, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return Status{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || len(p) > 3 {
			return Status{}, false
		}
		n[i] = v
	}
	if n[0] != 2 && n[0] != 4 && n[0] != 5 {
		return Status{}, false
	}
	return Status{Class: n[0], Subject: n[1], Detail: n[2]}, true
}

// Blame decides from a refusal's text (its enhanced status code and wording)
// what was refused. CauseUnknown means nothing in it is telling.
func Blame(text string) Cause {
	lower := strings.ToLower(text)
	status, hasStatus := StatusIn(lower)

	if hasStatus && status.Class == 4 {
		return CauseTransient
	}
	if hasStatus && status.Class == 5 && status.Subject == 2 && status.Detail == 2 || containsAny(lower, mailboxFullMarkers) {
		return CauseMailboxFull
	}
	if containsAny(lower, recipientMarkers) {
		return CauseRecipientInvalid
	}
	if containsAny(lower, transientMarkers) {
		return CauseTransient
	}
	if containsAny(lower, sessionMarkers) {
		return CauseReputationBlock
	}
	if !hasStatus || status.Class != 5 {
		return CauseUnknown
	}
	switch status.Subject {
	case 1:
		switch status.Detail {
		case 1, 2, 3, 6, 10: // bad mailbox, bad domain, bad syntax, moved, null MX
			return CauseRecipientInvalid
		case 7, 8: // the SENDER's address
			return CauseReputationBlock
		}
	case 2:
		switch status.Detail {
		case 1: // disabled
			return CauseRecipientInvalid
		case 3: // message too large
			return CauseReputationBlock
		}
	case 3: // receiving mail system
		return CauseTransient
	case 4:
		// Exchange answers a missing mailbox with 5.4.1 and a missing domain
		// with 5.4.4, so neither vouches for the address.
		if status.Detail != 1 && status.Detail != 4 {
			return CauseTransient
		}
	case 6, 7: // content, policy
		return CauseReputationBlock
	}
	return CauseUnknown
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
