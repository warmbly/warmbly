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

// NamesAddress reports a refusal that clearly says the address cannot receive
// mail, the only cause that may suppress it.
func (c Cause) NamesAddress() bool { return c == CauseRecipientInvalid }

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
// carries them (some Exim builds answer a missing mailbox with 554).
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
	"invalid recipient",
	"invalid mailbox",
	"mailbox not found",
	"mailbox is disabled",
	"mailbox disabled",
	"account has been disabled",
	"account is disabled",
	"unrouteable address",
	"unroutable address",
	"no mailbox here",
	"not a valid mailbox",
	"user doesn't have an account",
	"user doesn't have a ",
	"address not found",
	"address couldn't be found",
}

// weakRecipientMarkers blame the recipient only when nothing in the text
// blames the sender or a policy and the enhanced status does not place the
// refusal elsewhere: "550 5.7.1 mailbox unavailable" is a policy block, while
// Microsoft's "Recipient address rejected: Access denied" under 5.4.1 is not.
var weakRecipientMarkers = []string{
	"recipient address rejected",
	"recipient rejected",
	"mailbox unavailable",
	"does not exist",
	"doesn't exist",
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

// senderScopedMarkers scope a refusal to the SENDER's address or domain. They decide
// before any recipient wording, because Postfix's sender verification answers
// "Sender address rejected: User unknown in local recipient table".
var senderScopedMarkers = []string{
	"sender address",
	"sender domain",
	"sender's domain",
	"domain of sender",
	"envelope sender",
	"sender rejected",
	"sender verify failed",
	"sender not allowed",
	"mail from",
}

// sessionMarkers name the SENDER as the problem: its greeting, connecting IP,
// authentication or reputation, or a policy applied to the session. A refusal
// carrying one says nothing about the mailbox.
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
	"authentication required",
	"auth required",
	"not authenticated",
	"not authorized",
	"blacklist",
	"blocklist",
	"blocked using",
	"listed in",
	"spamhaus",
	"dnsbl",
	"listed at",
	"listed on",
	"listed by",
	"spam",
	"unsolicited",
	"reputation",
	"blocked",
	"banned",
	"spoof",
	"dmarc",
	"unauthenticated",
	"service unavailable",
	"connection refused",
	"policy",
}

// weakBlockMarkers refuse the message without saying who for, so they yield
// to recipient wording and to a status code: Exchange answers a missing
// mailbox "Recipient address rejected: Access denied".
var weakBlockMarkers = []string{
	"access denied",
	"message rejected",
	"rejected by",
	"not accepted",
}

// Status is an RFC 3463 enhanced status code (class.subject.detail).
type Status struct {
	Class, Subject, Detail int
}

// StatusIn returns the enhanced status code that decides a refusal: the first
// permanent (5.x.x) one, since a 4.x.x quoted beside it is earlier history,
// else the first other code; an undefined x.0.0 only when nothing is more
// specific. Tokens are whole, so an IP address never reads as one.
func StatusIn(text string) (Status, bool) {
	var permanent, specific, first Status
	var hasPermanent, hasSpecific, hasFirst bool
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' }) {
		s, ok := parseStatus(strings.Trim(tok, "."))
		if !ok {
			continue
		}
		if !hasFirst {
			first, hasFirst = s, true
		}
		if s.Subject == 0 && s.Detail == 0 {
			continue
		}
		if s.Class == 5 {
			permanent, hasPermanent = s, true
			break
		}
		if !hasSpecific {
			specific, hasSpecific = s, true
		}
	}
	switch {
	case hasPermanent:
		return permanent, true
	case hasSpecific:
		return specific, true
	}
	return first, hasFirst
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
	if containsAny(lower, senderScopedMarkers) {
		return CauseReputationBlock
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
	byStatus := CauseUnknown
	if hasStatus {
		byStatus = statusCause(status)
	}
	if containsAny(lower, weakRecipientMarkers) && !byStatus.AddressIsFine() {
		return CauseRecipientInvalid
	}
	if byStatus == CauseUnknown && containsAny(lower, weakBlockMarkers) {
		return CauseReputationBlock
	}
	return byStatus
}

// statusCause reads an enhanced status code on its own.
func statusCause(status Status) Cause {
	if status.Class != 5 {
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
