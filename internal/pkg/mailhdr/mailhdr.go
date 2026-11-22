// Package mailhdr encodes RFC 5322 header values.
//
// Headers are ASCII by protocol: a raw "é" or an emoji in a Subject or a
// display name is a spec violation that mail servers and clients render as
// mojibake. Every outbound transport (SMTP, Gmail, Graph) routes its header
// values through here so non-ASCII survives the trip as RFC 2047 encoded-words.
package mailhdr

import (
	"errors"
	"mime"
	"net/mail"
	"strings"

	"github.com/emersion/go-message/charset"
)

// wordDecoder reads RFC 2047 encoded-words back into UTF-8. The charset hook
// covers the legacy encodings Go does not handle natively (windows-1252,
// iso-2022-jp, gbk, and friends).
var wordDecoder = mime.WordDecoder{CharsetReader: charset.Reader}

// Subject encodes an unstructured header value. It is a no-op for pure ASCII,
// so ordinary subjects stay human-readable on the wire.
func Subject(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}

// ReplySubject is the subject a reply carries: "Re: " once, never stacked.
func ReplySubject(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	return "Re: " + s
}

// Address encodes one address entry, which may be bare ("a@b.com") or carry a
// display name ("Ana Rodríguez <a@b.com>"). Anything that is not exactly one
// address encodes to "", so it never reaches a header.
func Address(s string) string {
	parsed, ok := parseOne(s)
	if !ok {
		return ""
	}
	if parsed.Name == "" {
		// Keep a plain address plain; String would wrap it in angle brackets.
		return parsed.Address
	}
	// mail.Address.String RFC 2047-encodes a non-ASCII display name and quotes
	// one containing specials.
	return parsed.String()
}

// ValidAddress reports whether s is exactly one address, with or without a
// display name, in a form Address can encode.
func ValidAddress(s string) bool {
	_, ok := parseOne(s)
	return ok
}

// parseOne reads one address entry, accepting the legacy "Ana (a@b.com)" form
// through Bare and refusing any control character.
func parseOne(s string) (*mail.Address, bool) {
	s = strings.TrimSpace(s)
	if s == "" || hasControl(s) {
		return nil, false
	}
	// A pasted list is several recipients, never one.
	if list, err := mail.ParseAddressList(s); err == nil && len(list) > 1 {
		return nil, false
	}
	if parsed, err := mail.ParseAddress(s); err == nil {
		return parsed, true
	}
	if b := Bare(s); b != s {
		if parsed, err := mail.ParseAddress(b); err == nil && parsed.Name == "" {
			return parsed, true
		}
	}
	return nil, false
}

// ValidMessageID reports whether s is one Message-ID, bracketed or not:
// printable ASCII with no whitespace and no angle brackets inside.
func ValidMessageID(s string) bool {
	id := strings.TrimSpace(s)
	if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") {
		id = id[1 : len(id)-1]
	}
	if id == "" || len(id) > maxHeaderValue {
		return false
	}
	for i := 0; i < len(id); i++ {
		if c := id[i]; c <= ' ' || c > '~' || c == '<' || c == '>' {
			return false
		}
	}
	return true
}

// maxHeaderValue is RFC 5322's line limit, the most one unfolded value can be.
const maxHeaderValue = 998

// ErrUnsafeHeader is returned for a header that cannot be written as one line.
var ErrUnsafeHeader = errors.New("mail header name or value is not a single line")

// CheckHeader refuses a header whose name is not a field name or whose value
// carries CR, LF or NUL, any of which would end the header early.
func CheckHeader(name, value string) error {
	if name == "" {
		return ErrUnsafeHeader
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c <= ' ' || c > '~' || c == ':' {
			return ErrUnsafeHeader
		}
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return ErrUnsafeHeader
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < ' ' || r == 0x7f {
			return true
		}
	}
	return false
}

// AddressList encodes a To/Cc/Bcc header value from its entries. Empty entries
// are skipped so a stray "" never produces a trailing comma.
func AddressList(addrs []string) string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if enc := Address(a); enc != "" {
			out = append(out, enc)
		}
	}
	return strings.Join(out, ", ")
}

// Bare strips a display name down to the routable address ("Ana <a@b.com>" ->
// "a@b.com"). SMTP envelope commands take the address alone; a display name in
// RCPT TO is a syntax error and the server rejects the recipient.
//
// It also reads "Ana (a@b.com)", which is what the IMAP sync stored for every
// address until v0.4.25 and what older rows and older workers still carry. Any
// reader that compares a stored address against a mailbox or a contact has to
// go through here; the reply path did not, and every reply into an IMAP
// mailbox stopped counting the day address checks were added there.
func Bare(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if parsed, err := mail.ParseAddress(s); err == nil {
		return parsed.Address
	}
	if i := strings.LastIndex(s, "<"); i != -1 {
		if j := strings.Index(s[i:], ">"); j != -1 {
			return strings.TrimSpace(s[i+1 : i+j])
		}
	}
	if i := strings.LastIndex(s, "("); i != -1 {
		if j := strings.Index(s[i:], ")"); j != -1 {
			if inner := strings.TrimSpace(s[i+1 : i+j]); strings.Contains(inner, "@") {
				return inner
			}
		}
	}
	return s
}

// BareList maps Bare over a recipient list, dropping empties.
func BareList(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if b := Bare(a); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// DecodeWords turns any RFC 2047 encoded-words in a header value back into the
// text they stand for. Providers that hand back raw headers (Gmail's API does)
// otherwise surface "=?utf-8?q?caf=C3=A9?=" to the reader. Undecodable input
// is returned unchanged rather than dropped.
func DecodeWords(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	decoded, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}
