package quickmail

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

var dialect = func() outreachsource.Dialect {
	f := outreachsource.CommonFields()
	f["inbox_name"] = "Sender.Name"
	f["inbox_friendly_name"] = "Sender.FirstName"
	f["inbox_email"] = "Sender.Email"
	f["inbox_signature"] = ""
	return outreachsource.Dialect{Fields: f}
}()

var (
	spinRE   = regexp.MustCompile(`(?s)\(\(\s*(.*?)\s*\)\)`)
	dayRE    = regexp.MustCompile(`\{\{\s*=\s*b?day\s*[+-]?\s*\d*\s*\}\}`)
	tokenRE  = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)
	filterRE = regexp.MustCompile(`^[a-z_]+(\s*:.*)?$`)
)

// liquidFilters are Liquid filters that change a value's form; Warmbly has no
// equivalent, so they are dropped and the value is used as it is.
var liquidFilters = map[string]bool{
	"capitalize": true, "downcase": true, "upcase": true, "strip": true, "lstrip": true,
	"rstrip": true, "escape": true, "truncate": true, "truncatewords": true, "plus": true,
	"minus": true, "times": true, "divided_by": true, "append": true, "prepend": true,
	"replace": true, "remove": true, "strip_html": true, "url_encode": true, "date": true,
}

// prefixes QuickMail puts on lead fields, longest first; Warmbly's custom
// fields carry the bare name.
var prefixes = []string{"lead.custom.", "prospect.custom.", "lead.", "prospect."}

type translated struct {
	text  string
	notes []string
}

// translate rewrites QuickMail copy into Warmbly's template syntax.
func translate(s string) translated {
	var out translated
	s = spinRE.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.Split(spinRE.FindStringSubmatch(m)[1], "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return "{" + strings.Join(parts, "|") + "}"
	})
	if dayRE.MatchString(s) {
		s = dayRE.ReplaceAllString(s, "")
		out.notes = append(out.notes, "QuickMail's day of the week attributes have no Warmbly equivalent and were removed.")
	}
	if strings.Contains(s, "{%") {
		out.notes = append(out.notes, "QuickMail's Liquid conditions are kept as written and need rewriting by hand.")
	}
	filtered := false
	s = tokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		parts := strings.Split(html.UnescapeString(tokenRE.FindStringSubmatch(tok)[1]), "|")
		name := strings.TrimSpace(parts[0])
		custom := false
		for _, p := range prefixes {
			if len(name) > len(p) && strings.EqualFold(name[:len(p)], p) {
				name, custom = name[len(p):], strings.HasSuffix(p, "custom.")
				break
			}
		}
		segs := []string{name}
		for _, seg := range parts[1:] {
			seg = strings.TrimSpace(seg)
			word := strings.TrimSpace(strings.SplitN(seg, ":", 2)[0])
			switch {
			case seg == "":
			case word == "default":
				segs = append(segs, unquote(strings.TrimSpace(strings.SplitN(seg, ":", 2)[1])))
			case liquidFilters[word] && filterRE.MatchString(seg):
				filtered = true
			default:
				segs = append(segs, unquote(seg))
			}
		}
		if ref, ok := customRef(name, segs[1:]); custom && ok {
			return ref
		}
		return "{{" + strings.Join(segs, " | ") + "}}"
	})
	if filtered {
		out.notes = append(out.notes, "QuickMail's Liquid filters (like capitalize) were dropped.")
	}
	t := outreachsource.Translate(s, dialect)
	if len(t.Dropped) > 0 {
		out.notes = append(out.notes, "The inbox signature token was removed; Warmbly adds the mailbox signature itself.")
	}
	out.text = t.Text
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// customRef reads a custom property directly, so a property that shares a
// built-in's spelling (Job_Title) still reads its own value. A key Go
// templates cannot name with a dot is left to Translate.
func customRef(name string, fallbacks []string) (string, bool) {
	key := outreachsource.FieldKey(name)
	if key == "" || key[0] < 'a' || key[0] > 'z' {
		return "", false
	}
	if len(fallbacks) > 0 {
		return fmt.Sprintf("{{.%s | default %q}}", key, fallbacks[0]), true
	}
	return "{{." + key + "}}", true
}
