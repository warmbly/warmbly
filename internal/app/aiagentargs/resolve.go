// Package aiagentargs holds the pure argument-resolution logic shared by the
// campaign and automation AI agent steps, so both engines resolve a model's
// tag/label/pipeline choices to ids identically. It has no service
// dependencies: callers pass their own owner-scoped lists and a create closure.
package aiagentargs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// TagEnum returns the tag/label names to offer the model. A configured pool is
// the restriction; an empty pool falls back to every live category (tags and
// unibox labels are the same registry). Empty result means "no options".
func TagEnum(pool []models.AITagRef, live []models.MiniCategory) []string {
	if len(pool) > 0 {
		out := make([]string, 0, len(pool))
		for _, r := range pool {
			if n := strings.TrimSpace(r.Name); n != "" {
				out = append(out, n)
			}
		}
		return out
	}
	out := make([]string, 0, len(live))
	for _, c := range live {
		if n := strings.TrimSpace(c.Title); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ResolveTag maps the model's picked name to a category id. A non-empty pool is
// a hard restriction (pick must be in it); an empty pool matches against the
// live list and, if still unmatched and allowCreate is set, mints a new one via
// create. Case-insensitive throughout.
func ResolveTag(pool []models.AITagRef, live []models.MiniCategory, allowCreate bool, pick string, create func(title string) (uuid.UUID, error)) (uuid.UUID, error) {
	name := strings.TrimSpace(pick)
	if name == "" {
		return uuid.Nil, fmt.Errorf("no tag name given")
	}
	if len(pool) > 0 {
		for _, r := range pool {
			if strings.EqualFold(strings.TrimSpace(r.Name), name) {
				id, err := uuid.Parse(strings.TrimSpace(r.ID))
				if err != nil {
					return uuid.Nil, fmt.Errorf("tag %q is misconfigured", name)
				}
				return id, nil
			}
		}
		return uuid.Nil, fmt.Errorf("%q is not one of the allowed tags", name)
	}
	for _, c := range live {
		if strings.EqualFold(strings.TrimSpace(c.Title), name) {
			return c.ID, nil
		}
	}
	if allowCreate && create != nil {
		return create(name)
	}
	return uuid.Nil, fmt.Errorf("%q is not one of your tags", name)
}

// ResolvePipelineStage maps pipeline/stage names to ids, defaulting to the first
// pipeline and its first stage (both already position-ordered) when a name is
// blank. Errors when the org has no pipelines, so the agent gets a clear message
// instead of a zero-uuid FK violation.
func ResolvePipelineStage(pipelines []models.Pipeline, pipelineName, stageName string) (pipelineID, stageID uuid.UUID, err error) {
	if len(pipelines) == 0 {
		return uuid.Nil, uuid.Nil, fmt.Errorf("no CRM pipelines exist; create one first")
	}
	pl := pipelines[0]
	if n := strings.TrimSpace(pipelineName); n != "" {
		found := false
		for _, p := range pipelines {
			if strings.EqualFold(strings.TrimSpace(p.Name), n) {
				pl = p
				found = true
				break
			}
		}
		if !found {
			return uuid.Nil, uuid.Nil, fmt.Errorf("%q is not one of your pipelines", n)
		}
	}
	if len(pl.Stages) == 0 {
		return uuid.Nil, uuid.Nil, fmt.Errorf("pipeline %q has no stages", pl.Name)
	}
	st := pl.Stages[0]
	if n := strings.TrimSpace(stageName); n != "" {
		found := false
		for _, s := range pl.Stages {
			if strings.EqualFold(strings.TrimSpace(s.Name), n) {
				st = s
				found = true
				break
			}
		}
		if !found {
			return uuid.Nil, uuid.Nil, fmt.Errorf("%q is not a stage in pipeline %q", n, pl.Name)
		}
	}
	return pl.ID, st.ID, nil
}

// MatchValueToCases maps a rendered "value" (a template output or event field)
// onto a switch's case set for the deterministic, no-model "value" decider:
// normalized equality on plain cases first (case- and whitespace-insensitive),
// then "/pattern/" regex cases in configured order. First match wins; "" on a
// miss routes down the fallback path. Deliberately NO prefix/substring fuzziness
// ("not interested" must never land on an "interested" case). Shared verbatim by
// the campaign switch step and the automation AI switch so both route identically.
func MatchValueToCases(value string, cases []string) string {
	norm := normalizeSwitchText(value)
	if norm == "" {
		return ""
	}
	for _, c := range cases {
		if caseRegex(c) == nil && normalizeSwitchText(c) == norm {
			return c
		}
	}
	trimmed := strings.TrimSpace(value)
	for _, c := range cases {
		if re := caseRegex(c); re != nil && re.MatchString(trimmed) {
			return c
		}
	}
	return ""
}

// normalizeSwitchText lowercases, trims, and collapses inner whitespace so
// "VIP  Customer " matches the case "vip customer". Value matching must not fail
// on casing or stray spaces in the rendered value.
func normalizeSwitchText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// caseRegex compiles a "/pattern/" case name into a case-insensitive regex, or
// returns nil for a plain-text case (or an invalid pattern — the write-time
// validator rejects those, so nil here only means "not a regex").
func caseRegex(name string) *regexp.Regexp {
	name = strings.TrimSpace(name)
	if len(name) < 3 || !strings.HasPrefix(name, "/") || !strings.HasSuffix(name, "/") {
		return nil
	}
	re, err := regexp.Compile("(?i)" + name[1:len(name)-1])
	if err != nil {
		return nil
	}
	return re
}
