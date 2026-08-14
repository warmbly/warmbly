// Package emailaddr normalizes sender addresses from Unibox / IMAP From forms.
package emailaddr

import (
	"net/mail"
	"regexp"
	"strings"
)

// Hostinger and some IMAP fold paths store From as " (a@b)" or "Name (a@b)"
// instead of RFC 5322 "Name <a@b>". mail.ParseAddress fails on those forms.
var parenEmail = regexp.MustCompile(`(?i)\(\s*([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})\s*\)`)

// Extract returns a lowercased bare email from a single From token, or "".
// Accepts RFC 5322 display forms, angle-addr, bare addr, and live Hostinger
// Unibox forms " (a@b)" / "Name (a@b)".
func Extract(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if parsed, err := mail.ParseAddress(s); err == nil {
		return strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	// Parenthetical address: " (a@b)" or "Display Name (a@b)"
	if m := parenEmail.FindStringSubmatch(s); len(m) == 2 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	// Bare angle-addr without display name already failed ParseAddress above
	// only when malformed; try stripping surrounding <> and re-parse.
	trimmed := strings.TrimSpace(strings.Trim(s, "<>"))
	if trimmed != s {
		if parsed, err := mail.ParseAddress(trimmed); err == nil {
			return strings.ToLower(strings.TrimSpace(parsed.Address))
		}
		if looksLikeEmail(trimmed) {
			return strings.ToLower(trimmed)
		}
	}
	if looksLikeEmail(s) {
		return strings.ToLower(s)
	}
	return ""
}

// ExtractFirst returns Extract of the first non-empty token in addrs.
func ExtractFirst(addrs []string) string {
	for _, a := range addrs {
		if out := Extract(a); out != "" {
			return out
		}
	}
	return ""
}

func looksLikeEmail(s string) bool {
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return false
	}
	if strings.ContainsAny(s, " \t()<>\"") {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}
