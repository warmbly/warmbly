package integration

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"

	"github.com/warmbly/warmbly/internal/app/webhook"
)

// Templating for automation/integration action values (message bodies, channels,
// webhook URLs, CRM static field values). Renders Go text/template against the
// flat event-data map so users can write {{.contact_email}} (or bare
// {{contact_email}}) plus conditionals/pipelines. Unknown keys render empty.
// Never hard-fails: any parse/exec error falls back to naive {{key}}
// substitution, preserving the simple syntax users already typed.

var tmplCache sync.Map // string -> *template.Template, or the badTemplate sentinel

// Bound the cache so an attacker can't grow it without limit via many distinct
// template strings. Templates are config-derived (small in practice); beyond the
// cap we simply recompile on miss instead of caching — correctness is unchanged.
var tmplCacheCount atomic.Int64

const tmplCacheCap = 4096

var badTemplate = &template.Template{}

// bareKeyRe matches a standalone {{ identifier }} action (no leading dot, no
// spaces inside the name) so we can rewrite it to {{ .identifier }}. Pipelines,
// dotted fields, and control actions ({{if ...}}) are left untouched.
var bareKeyRe = regexp.MustCompile(`{{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*}}`)

// renderOutboundURL renders a (possibly templated) outbound webhook URL and
// re-validates the result against the SSRF/HTTPS guard. A non-empty input that
// renders to empty is treated as a misconfiguration (error), not a silent skip.
func renderOutboundURL(raw string, data map[string]any) (string, error) {
	url := renderTemplate(raw, data)
	if url == "" {
		return "", fmt.Errorf("webhook url rendered empty")
	}
	if err := webhook.ValidateOutboundURL(url); err != nil {
		return "", fmt.Errorf("rendered webhook url failed validation: %w", err)
	}
	return url, nil
}

// renderTemplate renders tmpl against the event data map.
func renderTemplate(tmpl string, data map[string]any) string {
	if !strings.Contains(tmpl, "{{") {
		return strings.TrimSpace(tmpl)
	}
	t := compileTemplate(tmpl)
	if t == nil {
		return naiveRenderTemplate(tmpl, data)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, flattenForTemplate(data)); err != nil {
		return naiveRenderTemplate(tmpl, data)
	}
	return strings.TrimSpace(buf.String())
}

func compileTemplate(tmpl string) *template.Template {
	if v, ok := tmplCache.Load(tmpl); ok {
		if v == badTemplate {
			return nil
		}
		return v.(*template.Template)
	}
	t, err := template.New("action").Option("missingkey=zero").Parse(bareKeyRe.ReplaceAllString(tmpl, "{{.$1}}"))
	if err != nil {
		cacheStore(tmpl, badTemplate)
		return nil
	}
	cacheStore(tmpl, t)
	return t
}

// cacheStore stores a compiled template unless the cache is at capacity (a soft
// cap — a small overshoot under concurrency is fine; the point is bounded growth).
func cacheStore(tmpl string, t *template.Template) {
	if tmplCacheCount.Load() >= tmplCacheCap {
		return
	}
	if _, loaded := tmplCache.LoadOrStore(tmpl, t); !loaded {
		tmplCacheCount.Add(1)
	}
}

// flattenForTemplate renders every event-data value to a string so templates see
// a uniform map[string]string (dot access works; missing keys are "").
func flattenForTemplate(data map[string]any) map[string]string {
	out := make(map[string]string, len(data))
	for k, v := range data {
		out[k] = valueString(v)
	}
	return out
}

// naiveRenderTemplate is the original literal {{key}} substitution, used as a
// safe fallback when a template can't compile/execute.
func naiveRenderTemplate(tmpl string, data map[string]any) string {
	out := tmpl
	for {
		start := strings.Index(out, "{{")
		if start < 0 {
			break
		}
		end := strings.Index(out[start:], "}}")
		if end < 0 {
			break
		}
		end += start
		key := strings.TrimSpace(out[start+2 : end])
		key = strings.TrimPrefix(key, ".")
		out = out[:start] + stringFromMap(data, key) + out[end+2:]
	}
	return strings.TrimSpace(out)
}
