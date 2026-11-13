package lemlist

import (
	"regexp"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

var dialect = func() source.Dialect {
	f := source.CommonFields()
	// lemlist's own spellings: {{sender.name}} normalises to sender_name already.
	f["unsubscribe_url"] = "unsubscribe_link"
	f["company_domain"] = "website"
	return source.Dialect{Fields: f}
}()

var (
	spinRE      = regexp.MustCompile(`(?s)\{%-?\s*spin\s*-?%\}(.*?)\{%-?\s*endspin\s*-?%\}`)
	variationRE = regexp.MustCompile(`\{%-?\s*variation\s*-?%\}`)
	liquidRE    = regexp.MustCompile(`\{%.*?%\}`)
)

// translate rewrites lemlist's Liquid spin blocks as Warmbly spintax, then its
// merge tokens. It reports dropped tokens and whether other Liquid remains.
func translate(text string) (string, []string, bool) {
	text = spinRE.ReplaceAllStringFunc(text, func(block string) string {
		inner := spinRE.FindStringSubmatch(block)[1]
		var opts []string
		for _, o := range variationRE.Split(inner, -1) {
			if o = strings.TrimSpace(o); o != "" {
				opts = append(opts, o)
			}
		}
		if len(opts) == 0 {
			return ""
		}
		return "{" + strings.Join(opts, "|") + "}"
	})
	t := source.Translate(text, dialect)
	return t.Text, t.Dropped, liquidRE.MatchString(t.Text)
}
