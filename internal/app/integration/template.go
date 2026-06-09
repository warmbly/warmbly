package integration

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/pkg/tmplfuncs"
)

// exprFuncs is the shared customization helper set (arithmetic, coercing numeric
// comparison, string helpers, default/fallback) available inside automation
// condition expressions and action templates. The built-in eq/ne/lt/le/gt/ge and
// and/or/not also work (natively when a value is already a number; use the *f
// variants or num to force numeric comparison on strings). Shared with campaign
// email bodies via internal/pkg/tmplfuncs.
var exprFuncs = tmplfuncs.FuncMap()

var exprTmplCache sync.Map // expr string -> *template.Template, or badTemplate

// prepExpr lets a user write either a full template (`{{if gt .x 1}}y{{end}}`)
// or a bare boolean pipeline (`gt .x 1`), which we wrap in an {{if}}.
func prepExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if !strings.Contains(expr, "{{") {
		return "{{if " + expr + "}}true{{end}}"
	}
	return expr
}

func compileExpr(expr string) *template.Template {
	if v, ok := exprTmplCache.Load(expr); ok {
		if v == badTemplate {
			return nil
		}
		return v.(*template.Template)
	}
	t, err := template.New("cond").Funcs(exprFuncs).Option("missingkey=zero").Parse(prepExpr(expr))
	if err != nil {
		exprTmplCache.Store(expr, badTemplate)
		return nil
	}
	exprTmplCache.Store(expr, t)
	return t
}

// EvalExpression renders a condition expression against the NATIVE event data
// (numbers stay numbers) and reports whether it is "truthy" — i.e. renders a
// non-empty, non-false value. Any parse/exec failure is a false (a broken
// condition never silently passes).
func EvalExpression(expr string, data map[string]any) bool {
	if strings.TrimSpace(expr) == "" {
		return false
	}
	t := compileExpr(expr)
	if t == nil {
		return false
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(buf.String())) {
	case "", "false", "0", "no", "off", "<no value>":
		return false
	}
	return true
}

// ValidExpression reports whether a condition expression parses (used on write,
// so a campaign can't be saved with a broken predicate).
func ValidExpression(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("expression is empty")
	}
	if _, err := template.New("cond").Funcs(exprFuncs).Option("missingkey=zero").Parse(prepExpr(expr)); err != nil {
		return err
	}
	return nil
}

// Templating for automation/integration action values (message bodies, channels,
// webhook URLs, CRM static field values). Renders the FULL, standard Go
// text/template engine against the NATIVE event-data map — the same data
// conditions evaluate against — so an action value can do everything a Go
// template can, not just substitute a value: conditionals/else, {{range}} over
// lists, {{with}}, nested dotted access ({{.lead.company}}), pipelines, and the
// shared helper funcs, with numbers and booleans keeping their type (so native
// {{if gt .confidence 0.8}} works, no coercion needed). Variables use standard
// dotted field access ({{.contact_email}}); there is no bare-{{key}} shorthand —
// the template is plain Go text/template. Unknown keys render empty. Never
// hard-fails: any parse/exec error falls back to naive {{.key}} substitution.

var tmplCache sync.Map // string -> *template.Template, or the badTemplate sentinel

// Bound the cache so an attacker can't grow it without limit via many distinct
// template strings. Templates are config-derived (small in practice); beyond the
// cap we simply recompile on miss instead of caching — correctness is unchanged.
var tmplCacheCount atomic.Int64

const tmplCacheCap = 4096

var badTemplate = &template.Template{}

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

// renderTemplate renders tmpl against the native event data map.
func renderTemplate(tmpl string, data map[string]any) string {
	if !strings.Contains(tmpl, "{{") {
		return strings.TrimSpace(tmpl)
	}
	t := compileTemplate(tmpl)
	if t == nil {
		return naiveRenderTemplate(tmpl, data)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return naiveRenderTemplate(tmpl, data)
	}
	return strings.TrimSpace(stripNoValue(buf.String()))
}

// stripNoValue removes the text/template "<no value>" sentinel that
// missingkey=zero emits for an absent key on a map[string]any (its element type
// is interface{}, whose zero value prints as that sentinel). Stripping it keeps
// the documented contract — an unknown placeholder renders empty — instead of
// leaking "<no value>" into a customer-facing Slack/webhook/CRM value.
func stripNoValue(s string) string {
	if !strings.Contains(s, "<no value>") {
		return s
	}
	return strings.ReplaceAll(s, "<no value>", "")
}

func compileTemplate(tmpl string) *template.Template {
	if v, ok := tmplCache.Load(tmpl); ok {
		if v == badTemplate {
			return nil
		}
		return v.(*template.Template)
	}
	t, err := template.New("action").Funcs(exprFuncs).Option("missingkey=zero").Parse(tmpl)
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

// naiveRenderTemplate is a literal {{.key}} substitution (a leading dot is
// optional here), used as a safe fallback when a template can't compile/execute
// so a single malformed block never blanks the whole value.
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
