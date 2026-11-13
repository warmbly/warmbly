package smartlead

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// Smartlead's lead tokens (company_name, phone_number, website, location,
// linkedin_profile, company_url) are all common spellings.
var dialect = source.Dialect{Fields: source.CommonFields()}

// percentTokens are Smartlead's sender placeholders, written %name%.
// An empty value is removed: Warmbly adds a signature itself, and has no
// sender domain or source campaign id to put there.
var percentTokens = map[string]string{
	"%sender-name%":      "{{.Sender.Name}}",
	"%sender-firstname%": "{{.Sender.FirstName}}",
	"%sender-mailbox%":   "{{.Sender.Email}}",
	"%unsubscribe-text%": "{{unsubscribe_link}}",
	"%signature%":        "",
	"%sender-domain%":    "",
	"%campaign-id%":      "",
}

var (
	percentRE = regexp.MustCompile(`(?i)%(sender-name|sender-firstname|sender-mailbox|sender-domain|unsubscribe-text|signature|campaign-id)%`)
	tokenRE   = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)
	// ifRE is Smartlead's handlebars condition: `#if name` or `#if name 'op' value`.
	ifRE = regexp.MustCompile(`^#(if|unless)\s+([^\s']+)(?:\s+'([^']+)'\s+(.+))?$`)
)

// translated is a template in Warmbly's syntax plus what did not carry over.
type translated struct {
	Text    string
	Fields  []string
	Dropped []string
	// Kept are logic tokens Warmbly cannot read, left as written.
	Kept []string
}

// translate rewrites a Smartlead subject or body. Handlebars conditions
// become Go template conditions; `{{sl_...}}` send-time helpers are removed.
func translate(text string) translated {
	var out translated
	seen := map[string]bool{}
	addField := func(k string) {
		if !seen[k] {
			seen[k] = true
			out.Fields = append(out.Fields, k)
		}
	}
	text = percentRE.ReplaceAllStringFunc(text, func(tok string) string {
		repl := percentTokens[strings.ToLower(tok)]
		if repl == "" {
			out.Dropped = append(out.Dropped, strings.ToLower(tok))
		}
		return repl
	})
	// Logic tags are parked behind placeholders so Translate leaves them alone.
	var parked []string
	park := func(s string) string {
		parked = append(parked, s)
		return "" + strconv.Itoa(len(parked)-1) + ""
	}
	text = tokenRE.ReplaceAllStringFunc(text, func(tok string) string {
		inner := strings.TrimSpace(strings.ReplaceAll(html.UnescapeString(tokenRE.FindStringSubmatch(tok)[1]), " ", " "))
		lower := strings.ToLower(inner)
		switch {
		case lower == "else":
			return park("{{else}}")
		case lower == "/if", lower == "/unless":
			return park("{{end}}")
		case strings.HasPrefix(lower, "sl_"):
			out.Dropped = append(out.Dropped, tok)
			return ""
		case strings.HasPrefix(lower, "#"):
			cond, field, ok := condition(inner)
			if !ok {
				out.Kept = append(out.Kept, tok)
				return park(tok)
			}
			if field != "" {
				addField(field)
			}
			return park("{{if " + cond + "}}")
		}
		return tok
	})
	t := source.Translate(text, dialect)
	for _, f := range t.Fields {
		addField(f)
	}
	out.Dropped = append(out.Dropped, t.Dropped...)
	res := t.Text
	for i, p := range parked {
		res = strings.Replace(res, ""+strconv.Itoa(i)+"", p, 1)
	}
	out.Text = res
	return out
}

// condition turns `#if name 'op' value` into a Go template pipeline, with the
// custom field it reads ("" for a built-in).
func condition(inner string) (cond, custom string, ok bool) {
	m := ifRE.FindStringSubmatch(inner)
	if m == nil {
		return "", "", false
	}
	key := source.FieldKey(m[2])
	field, known := dialect.Fields[key]
	switch {
	case key == "", known && (field == "" || field == "unsubscribe_link"):
		return "", "", false
	case !known:
		field, custom = key, key
	}
	ref := "." + field
	expr := ref
	if m[3] != "" {
		val := literal(m[4])
		switch m[3] {
		case "==":
			expr = "eq (trim " + ref + ") " + strconv.Quote(val)
		case "!=":
			expr = "ne (trim " + ref + ") " + strconv.Quote(val)
		case ">":
			expr = "gtf " + ref + " " + number(val)
		case "<":
			expr = "ltf " + ref + " " + number(val)
		case "gte":
			expr = "gef " + ref + " " + number(val)
		case "lte":
			expr = "lef " + ref + " " + number(val)
		default:
			return "", "", false
		}
	}
	if m[1] == "unless" {
		expr = "not (" + expr + ")"
	}
	return expr, custom, true
}

func literal(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

func number(v string) string {
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	return strconv.Quote(v)
}
