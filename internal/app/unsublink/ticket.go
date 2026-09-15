package unsublink

import (
	"crypto/rand"
	"encoding/base64"
)

// A ticket is the short form of an unsubscribe link: a random token that
// names nothing on its own and is resolved against a stored row, rather than
// a signed blob carrying its own claims.
//
// The reason is length, and length matters here more than anywhere else a
// link appears. The text/plain alternative of a message has no anchor to put
// a word in, so the opt-out address is the one URL a recipient reads in full,
// and a plain-text campaign carries no HTML part at all (issue #498). The
// signed token has to hold three ids, an expiry and a MAC, which is 96
// base64 characters; a ticket is 22, so the whole address fits on one line
// and reads as an address rather than a tracking parameter.
//
// Signed tokens stay honoured: they were minted with a year of validity, so
// links already in inboxes go on working, and one is still what ships when a
// ticket cannot be stored.
const (
	// TicketBytes is 128 bits of entropy from crypto/rand, which is what
	// makes a token unguessable rather than merely long.
	//
	// The size is deliberate and it is not a UUID. A random UUID carries 122
	// bits (six go to the version and variant nibbles) and spends 36
	// characters doing it, because hex is four bits a character and four of
	// them are hyphens. Sixteen raw bytes in base64url are six bits a
	// character with no separators: more entropy than a UUID, in 22
	// characters instead of 36. Better on both axes, which is the only
	// reason to depart from the obvious choice.
	//
	// Why 128 and not the 72 bits that would fit in 12 characters: the
	// margin has to hold at scale, not just today. Guessing is a birthday
	// problem against every live token at once, so the odds improve as an
	// instance grows. At a billion live links and a million guesses a second
	// a 72-bit space yields its first hit in about two months; the same
	// assault on this one runs for 1e16 years. Ten characters is a cheap
	// price for never having to revisit that.
	TicketBytes = 16
	// TicketLen is TicketBytes in unpadded base64url.
	TicketLen = 22
)

// ExampleTicket stands in for a recipient's real ticket in a preview: the
// right shape and the right width, so a plain-text preview reads the same as
// the send, and it resolves to nothing.
const ExampleTicket = "exampleticket000000000"

// NewTicket mints a fresh ticket token.
func NewTicket() (string, error) {
	raw := make([]byte, TicketBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// IsTicket reports whether a token off a request has the ticket shape. The
// two link generations share one route, so this is what decides which way a
// token is resolved: a 96-character signed token and a spray of junk are both
// answered without a database lookup.
func IsTicket(token string) bool {
	if len(token) != TicketLen {
		return false
	}
	for i := 0; i < len(token); i++ {
		switch c := token[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
