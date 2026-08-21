// Package mailhdr encodes RFC 5322 header values.
//
// Headers are ASCII by protocol: a raw "é" or an emoji in a Subject or a
// display name is a spec violation that mail servers and clients render as
// mojibake. Every outbound transport (SMTP, Gmail, Graph) routes its header
// values through here so non-ASCII survives the trip as RFC 2047 encoded-words.
package mailhdr

import (
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

// Address encodes one address entry, which may be bare ("a@b.com") or carry a
// display name ("Ana Rodríguez <a@b.com>"). Unparseable input is returned
// trimmed but otherwise untouched: it is better to send what the caller meant
// than to drop a recipient.
func Address(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parsed, err := mail.ParseAddress(s)
	if err != nil {
		return s
	}
	if parsed.Name == "" {
		// Keep a plain address plain; String would wrap it in angle brackets.
		return parsed.Address
	}
	// mail.Address.String RFC 2047-encodes a non-ASCII display name and quotes
	// one containing specials.
	return parsed.String()
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
