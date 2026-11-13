package outreachsource

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Dialect is how one provider writes merge tokens. Every provider seen so far
// writes `{{name}}` with an optional fallback after a pipe, and some spell
// spintax as a token (`{{RANDOM | a | b}}`); that is what Translate reads.
type Dialect struct {
	// Fields maps a provider token, normalised with FieldKey, to the Warmbly
	// field it means: "FirstName", "Sender.Name". A token mapped to "" is
	// dropped, for things Warmbly adds itself (a signature). Anything not here
	// becomes the custom field of the same normalised name.
	Fields map[string]string
	// Spin names the token that holds spintax ("random"), normalised.
	Spin []string
	// FallbackSep separates a token's fallback from its name, "|" when empty.
	FallbackSep string
}

// CommonFields are the spellings most tools share for the built-in fields.
// A provider extends a copy of it.
func CommonFields() map[string]string {
	return map[string]string{
		"first_name": "FirstName", "firstname": "FirstName",
		"last_name": "LastName", "lastname": "LastName",
		"email": "Email", "email_address": "Email",
		"company": "Company", "company_name": "Company", "companyname": "Company",
		"phone": "Phone", "phone_number": "Phone",
		"sender_name": "Sender.Name", "sender_first_name": "Sender.FirstName",
		"sender_last_name": "Sender.LastName", "sender_email": "Sender.Email",
		"signature": "", "sender_signature": "", "account_signature": "",
		"unsubscribe_link": "unsubscribe_link", "unsubscribe": "unsubscribe_link",
		// Lead fields Warmbly stores as custom fields, under the keys the
		// migration writes them to (see LeadCustomKeys).
		"website": "website", "company_website": "website", "company_url": "website",
		"linkedin": "linkedin", "linkedin_profile": "linkedin", "linkedin_url": "linkedin", "linked_in_url": "linkedin",
		"location": "location", "city": "location",
		"title": "title", "job_title": "title", "jobtitle": "title",
		"timezone": "timezone", "time_zone": "timezone",
	}
}

var tokenRE = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// Translated is a template rewritten for Warmbly, with what could not carry over.
type Translated struct {
	Text string
	// Fields are the custom field keys the text now references.
	Fields []string
	// Dropped are tokens removed because Warmbly adds them itself.
	Dropped []string
}

// Translate rewrites a provider's merge tokens into Warmbly's syntax:
// `{{firstName|there}}` becomes `{{.FirstName | default "there"}}` and
// `{{RANDOM | Hi | Hey}}` becomes `{Hi|Hey}`. Single-brace spintax is already
// Warmbly's and is left alone.
func Translate(text string, d Dialect) Translated {
	sep := d.FallbackSep
	if sep == "" {
		sep = "|"
	}
	spin := map[string]bool{}
	for _, s := range d.Spin {
		spin[FieldKey(s)] = true
	}
	var out Translated
	seen := map[string]bool{}
	// Innermost tokens are translated first and parked behind placeholders, so
	// a token nested in spintax (`{{RANDOM | Hi {{firstName}} | Hey}}`) is
	// translated before the spintax around it.
	var parked []string
	park := func(s string) string {
		parked = append(parked, s)
		return fmt.Sprintf("\x00%d\x00", len(parked)-1)
	}
	translate := func(tok string) string {
		inner := html.UnescapeString(tokenRE.FindStringSubmatch(tok)[1])
		inner = strings.ReplaceAll(inner, " ", " ")
		if strings.HasPrefix(strings.TrimSpace(inner), ".") {
			return park(tok) // already Warmbly syntax
		}
		parts := strings.Split(inner, sep)
		name := strings.TrimSpace(parts[0])
		key := FieldKey(name)
		if spin[key] && len(parts) > 1 {
			opts := make([]string, 0, len(parts)-1)
			for _, p := range parts[1:] {
				opts = append(opts, strings.TrimSpace(p))
			}
			return park("{" + strings.Join(opts, "|") + "}")
		}
		if key == "" || strings.Contains(name, "\x00") {
			return park(tok)
		}
		field, known := d.Fields[key]
		if !known {
			field = key
			if !seen[key] {
				seen[key] = true
				out.Fields = append(out.Fields, key)
			}
		}
		if known && field == "" {
			out.Dropped = append(out.Dropped, name)
			return ""
		}
		if field == "unsubscribe_link" {
			return park("{{unsubscribe_link}}")
		}
		return park("{{" + chain(fieldRef(field), parts[1:], d.Fields) + "}}")
	}
	text = strings.ReplaceAll(text, "\x00", "")
	for tokenRE.MatchString(text) {
		text = tokenRE.ReplaceAllStringFunc(text, translate)
	}
	for i := len(parked) - 1; i >= 0; i-- {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00%d\x00", i), parked[i])
	}
	out.Text = text
	return out
}

// fieldRef is a template reference to a field, using index for a key that is
// not a valid identifier.
func fieldRef(field string) string {
	if field != "" && field[0] >= '0' && field[0] <= '9' {
		return fmt.Sprintf("(index . %q)", field)
	}
	return "." + field
}

// chain turns fallbacks into nested defaults. A fallback naming a built-in
// field reads that field (`{{firstName | lastName | there}}`); any other is
// literal text.
func chain(ref string, fallbacks []string, fields map[string]string) string {
	var expr string
	for i := len(fallbacks) - 1; i >= 0; i-- {
		f := strings.TrimSpace(fallbacks[i])
		if f == "" {
			continue
		}
		next := fmt.Sprintf("%q", f)
		if m, ok := fields[FieldKey(f)]; ok && m != "" && m != "unsubscribe_link" {
			next = "." + m
		}
		if expr == "" {
			expr = next
		} else {
			expr = fmt.Sprintf("(%s | default %s)", next, expr)
		}
	}
	if expr == "" {
		return ref
	}
	return ref + " | default " + expr
}
