// Package displayname is the one set of rules for a name a person chooses for
// themselves or their workspace. Those names are shown to other people,
// including in platform email, where a client turns anything that looks like
// an address into a live link under Warmbly's sender. So a name carries text
// and nothing a mail client, browser or terminal would act on.
package displayname

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/errx"
	"golang.org/x/text/unicode/norm"
)

// Kind selects the length bound and the minimum content of a name.
type Kind int

const (
	// Person is a first or last name.
	Person Kind = iota
	// Workspace is an organization name.
	Workspace
	// Heading is a short line of text one person writes for a page others open.
	Heading
	// Message is a sentence or two of the same.
	Message
)

const (
	PersonMaxLength    = 50
	WorkspaceMaxLength = 64
	HeadingMaxLength   = 60
	MessageMaxLength   = 200
	maxCombiningRun    = 3
)

// Rejection says why a name was refused.
type Rejection int

const (
	OK Rejection = iota
	Empty
	TooLong
	Link
	Characters
	NoLetter
)

// ErrorCode is the response `code` for every refused name.
const ErrorCode = "invalid_name"

var (
	schemeRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:https?|ftps?|mailto|tel|sms|callto|skype|javascript|data|file|news|irc|xmpp|ssh|git|ws|wss):\S`)
	// A label, a dot and a letter run: what linkifiers read as a hostname.
	domainRe = regexp.MustCompile(`[\p{L}\p{N}-]\.(?:[\p{L}]{2,}|xn--)`)
	ipv4Re   = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}`)
)

// Normalize trims the name and collapses every run of spaces to one ASCII
// space. It is the stored form of a name that passes Check.
func Normalize(s string) string {
	s = norm.NFC.String(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.Is(unicode.Zs, r) || r == ' ' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// Check normalizes s and applies every rule for kind.
func Check(s string, kind Kind) (string, Rejection) {
	s = Normalize(s)
	if s == "" {
		return "", Empty
	}
	if utf8.RuneCountInString(s) > maxLength(kind) {
		return s, TooLong
	}
	if r := content(s); r != OK {
		return s, r
	}
	if !hasLetter(s, kind) {
		return s, NoLetter
	}
	return s, OK
}

// Clean returns the normalized name when it passes Check, otherwise "". Use it
// for names that arrive from somewhere nobody can be asked to correct, such as
// an identity provider or an email address.
func Clean(s string, kind Kind) string {
	if out, r := Check(s, kind); r == OK {
		return out
	}
	return ""
}

// CleanOr is Clean after cutting s to kind's length on a rune boundary, or
// fallback when nothing passes. For a name nobody can be asked to shorten.
func CleanOr(s string, kind Kind, fallback string) string {
	r := []rune(Normalize(s))
	if n := maxLength(kind); len(r) > n {
		r = r[:n]
	}
	if out := Clean(string(r), kind); out != "" {
		return out
	}
	return fallback
}

// Displayable returns the normalized name when it is safe to show another
// person, otherwise "". It skips the length bound so a stored name that
// predates it still renders; the caller supplies the fallback.
func Displayable(s string) string {
	s = Normalize(s)
	if s == "" || content(s) != OK {
		return ""
	}
	return s
}

// DisplayableOr is Displayable with a neutral fallback.
func DisplayableOr(s, fallback string) string {
	if out := Displayable(s); out != "" {
		return out
	}
	return fallback
}

// FromEmail derives a first name from an address's local part, with the
// separators people use between names turned into spaces.
func FromEmail(address string) string {
	local, _, ok := strings.Cut(address, "@")
	if !ok {
		return ""
	}
	local, _, _ = strings.Cut(local, "+")
	local = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(local)
	if n := []rune(Normalize(local)); len(n) > PersonMaxLength {
		local = string(n[:PersonMaxLength])
	}
	return Clean(local, Person)
}

// Validate is Check answered as an API error naming the field. An empty value
// passes when optional is true.
func Validate(label, s string, kind Kind, optional bool) (string, *errx.Error) {
	out, r := Check(s, kind)
	if r == Empty && optional {
		return "", nil
	}
	if r == OK {
		return out, nil
	}
	return "", Error(label, kind, r)
}

// Error is the API error for a refused name.
func Error(label string, kind Kind, r Rejection) *errx.Error {
	var msg string
	switch r {
	case Empty:
		msg = label + " is required."
	case TooLong:
		msg = fmt.Sprintf("%s must be %d characters or less.", label, maxLength(kind))
	case Link:
		msg = label + " cannot contain a link, web address or email address."
	case NoLetter:
		if kind == Person {
			msg = label + " must contain a letter."
		} else {
			msg = label + " must contain a letter or a number."
		}
	default:
		msg = label + " contains characters that are not allowed."
	}
	return errx.NewWithIdentifier(errx.BadRequest, ErrorCode, msg)
}

func maxLength(kind Kind) int {
	switch kind {
	case Workspace:
		return WorkspaceMaxLength
	case Heading:
		return HeadingMaxLength
	case Message:
		return MessageMaxLength
	}
	return PersonMaxLength
}

func content(s string) Rejection {
	combining := 0
	for _, r := range s {
		switch {
		case r == utf8.RuneError,
			unicode.IsControl(r),
			unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs, unicode.Zl, unicode.Zp),
			strings.ContainsRune("<>`\\", r):
			return Characters
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
			combining++
			if combining > maxCombiningRun {
				return Characters
			}
		default:
			combining = 0
		}
	}
	if looksLikeLink(s) {
		return Link
	}
	return OK
}

// looksLikeLink reads the name the way a linkifier would, after folding the
// full-width and ideographic forms that resolve to the same address.
func looksLikeLink(s string) bool {
	f := strings.ToLower(norm.NFKC.String(s))
	f = strings.NewReplacer("\u3002", ".", "\uff61", ".").Replace(f)
	if strings.Contains(f, "@") || strings.Contains(f, "//") || strings.Contains(f, "www.") {
		return true
	}
	return schemeRe.MatchString(f) || domainRe.MatchString(f) || ipv4Re.MatchString(f)
}

func hasLetter(s string, kind Kind) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || (kind != Person && unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

// DefaultWorkspace names the workspace created for a new account.
func DefaultWorkspace(firstName string) string {
	if Normalize(firstName) == "" {
		return "My Organization"
	}
	if name := Clean(firstName+"'s Organization", Workspace); name != "" {
		return name
	}
	return "My Organization"
}

// FullName joins the displayable parts of a person's name, "" when neither is.
func FullName(first, last string) string {
	return strings.TrimSpace(Displayable(first) + " " + Displayable(last))
}
