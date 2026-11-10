package advisor

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks"
)

// copyDetectors read the actual email copy in a campaign's steps. They are
// deliberately conservative: copy advice that fires on good writing is worse
// than no copy advice, because it teaches people to dismiss the Advisor
// wholesale.
func copyDetectors() []Detector {
	return []Detector{
		{
			Key:      "copy_broken_template",
			Category: models.AdvisorCategoryCopy,
			About:    "Copy that the template engine cannot parse. On send it silently degrades to the naive replacement path, so conditionals and merge variables ship to the recipient as literal text. This is the single most visible mistake in cold outreach and instantly marks a message as bulk.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionSteps},
			Run:      detectBrokenTemplate,
		},
		{
			Key:      "copy_spam_phrases",
			Category: models.AdvisorCategoryCopy,
			About:    "Phrases that both spam filters and human readers treat as bulk-mail markers. The filter cost is real but secondary; the main cost is that these phrases make a message read as something nobody wrote to anyone in particular.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionSteps},
			Run:      detectSpamPhrases,
		},
		{
			Key:      "copy_too_many_links",
			Category: models.AdvisorCategoryCopy,
			About:    "Link count in a cold email. Multiple links in a first-touch email is a strong bulk-mail signal to filters, and it splits the reader's attention away from the single thing you want them to do.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionSteps},
			Run:      detectTooManyLinks,
		},
		{
			Key:      "copy_shouty_subject",
			Category: models.AdvisorCategoryCopy,
			About:    "Subject lines in capitals or stacked with exclamation marks. This is a bulk-mail signal for filters and reads as shouting to a person.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionSteps},
			Run:      detectShoutySubject,
		},
	}
}

var (
	// urlRe finds web addresses in either body form.
	urlRe = regexp.MustCompile(`(?i)https?://[^\s"'<>)]+`)
	// srcAttrRe drops image and script sources, which load rather than link.
	srcAttrRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	// wordRe splits on whitespace for a word count that does not need to be
	// exact, only stable.
	wordRe = regexp.MustCompile(`\S+`)
	// tagRe strips HTML so phrases are matched in prose, not markup.
	tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
	// styleRe removes script/style blocks before stripping tags, so their
	// contents do not count as words.
	styleRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
)

// sampleContact fills every built-in field, so copy is measured as a recipient
// reads it rather than as template source.
var sampleContact = models.Contact{
	FirstName: "Alex",
	LastName:  "Morgan",
	Email:     "alex@example.com",
	Company:   "Northwind",
	Phone:     "+1 555 0100",
}

// copyView is a step as its recipient reads it.
type copyView struct {
	// subject is empty for a step that replies under the conversation's subject.
	subject string
	// options is the body's text with every spintax option kept, for matching
	// phrases any recipient may get.
	options string
	// links is the body's distinct web addresses.
	links int
}

func viewOf(st repository.AdvisorStep, threaded bool) copyView {
	render := func(tmpl string) string {
		return tasks.RenderTemplateWith(tmpl, sampleContact, tasks.TemplateContext{})
	}
	plain, html := render(st.BodyPlain), render(st.BodyHTML)
	body := plain
	if strings.TrimSpace(body) == "" {
		body = tagRe.ReplaceAllString(styleRe.ReplaceAllString(html, " "), " ")
	}
	linkSource := plain
	if strings.TrimSpace(linkSource) == "" {
		linkSource = srcAttrRe.ReplaceAllString(html, " ")
	}
	// A recipient gets one option of each group, so count the one carrying the most links.
	linkSource = tasks.ResolveSpintax(linkSource, func(opts []string) string {
		best := opts[0]
		for _, o := range opts[1:] {
			if distinctLinks(o) > distinctLinks(best) {
				best = o
			}
		}
		return best
	})
	v := copyView{options: body, links: distinctLinks(linkSource)}
	if !threaded {
		v.subject = strings.TrimSpace(tasks.ShortestSpintax(render(st.Subject)))
	}
	return v
}

// distinctLinks counts each address once: an HTML anchor names its target in
// href and often again as its text.
func distinctLinks(body string) int {
	seen := map[string]bool{}
	for _, u := range urlRe.FindAllString(body, -1) {
		seen[strings.ToLower(strings.TrimRight(u, ".,;:!?"))] = true
	}
	return len(seen)
}

// stepContext pairs a step with its campaign for labelling.
type stepContext struct {
	step     repository.AdvisorStep
	campaign string
	status   string
	// first is the campaign's first email; threaded is whether the step
	// replies under the conversation's subject.
	first, threaded bool
	view            copyView
}

// emailSteps returns every email step in a non-draft campaign, with the
// campaign name attached. Draft campaigns are excluded: copy advice on a half
// written draft is noise, and the pre-send preflight covers that moment.
func emailSteps(s *repository.AdvisorSnapshot) []stepContext {
	names := map[string]string{}
	statuses := map[string]string{}
	for _, c := range s.Campaigns {
		names[c.ID.String()] = c.Name
		statuses[c.ID.String()] = c.Status
	}

	out := []stepContext{}
	opened := map[uuid.UUID]bool{} // campaigns whose conversation an earlier email opened
	for _, st := range s.Steps {
		if st.Kind != "email" {
			continue
		}
		first := !opened[st.CampaignID]
		threaded := st.ThreadReply && !first
		opened[st.CampaignID] = true
		status := statuses[st.CampaignID.String()]
		if status == "draft" || status == "" {
			continue
		}
		out = append(out, stepContext{step: st, campaign: names[st.CampaignID.String()], status: status, first: first, threaded: threaded, view: viewOf(st, threaded)})
	}
	return out
}

// stepLabel is the human name for a step in a finding.
func stepLabel(sc stepContext) string {
	name := sc.step.Name
	if name == "" {
		name = fmt.Sprintf("Step %d", sc.step.Position)
	}
	return name
}

// detectBrokenTemplate asks the platform's own template validator whether the
// copy parses, rather than pattern-matching for "suspicious" braces. Warmbly
// bodies are Go templates: `{{if .Company}}`, `{{index . "city"}}`, and
// `{{.FirstName | title}}` are all correct, and a regex written against a
// simpler `{{token}}` convention would flag every one of them. Reusing
// tasks.TemplateError means this fires exactly when a real send would fall back
// to literal text, and never otherwise.
func detectBrokenTemplate(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, sc := range emailSteps(s) {
		broken := map[string]string{}
		for field, text := range map[string]string{
			"subject":         sc.step.Subject,
			"body":            sc.step.BodyHTML,
			"plain-text body": sc.step.BodyPlain,
		} {
			if text == "" {
				continue
			}
			if err := tasks.TemplateError(text); err != nil {
				broken[field] = templateErrorSummary(err)
			}
		}
		if len(broken) == 0 {
			continue
		}

		// A template that cannot parse in a campaign that is already sending is
		// shipping literal braces to real people right now.
		severity := models.AdvisorHigh
		if sc.status != "active" {
			severity = models.AdvisorMedium
		}

		fields := make([]string, 0, len(broken))
		for field := range broken {
			fields = append(fields, field)
		}
		sort.Strings(fields)

		out = append(out, Finding{
			Key:         "copy_broken_template",
			GroupTitle:  "{count} steps have copy that will not render",
			Category:    models.AdvisorCategoryCopy,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "step",
			EntityID:    ref(sc.step.ID),
			EntityLabel: fmt.Sprintf("%s / %s", sc.campaign, stepLabel(sc)),
			ParentType:  "campaign",
			ParentID:    ref(sc.step.CampaignID),
			Impact:      clampImpact(70 + sc.step.Sent/50),
			Title:       fmt.Sprintf("%s has copy that will not render", stepLabel(sc)),
			Detail: fmt.Sprintf(
				"The %s in %s cannot be parsed as a template (%s). Sending does not fail on this: it falls back to plain replacement, so conditionals and merge variables go out to the recipient as literal text.",
				joinWords(fields), stepLabel(sc), broken[fields[0]]),
			Remedy: "Fix the template syntax. Check that every {{if}} has a matching {{end}}, that quotes are balanced, and that field names have no stray characters.",
			Steps: []string{
				fmt.Sprintf("Open the campaign and go to %s.", stepLabel(sc)),
				fmt.Sprintf("The parser stopped here: %s.", broken[fields[0]]),
				"Every {{if}} and {{range}} needs its own {{end}}. A missing {{end}} is the most common cause and the error usually points past it, not at it.",
				"Write a field with a leading dot, like {{.FirstName}}; the first_name and firstName spellings of built-in fields work too. A custom field is {{.city}}, or {{index . \"city\"}}.",
				"Save, then use the preview to check it renders against a real contact rather than trusting that it looks right.",
			},
			Evidence: map[string]any{
				"campaign":    sc.campaign,
				"step":        stepLabel(sc),
				"fields":      fields,
				"parse_error": broken[fields[0]],
				"step_sends":  sc.step.Sent,
				"status":      sc.status,
			},
		})
	}
	return out
}

// templateErrorSummary trims Go's template parse error down to the part that
// helps, dropping the internal template name prefix the user never chose.
func templateErrorSummary(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "template: ") {
		if j := strings.Index(msg[i+2:], ": "); j >= 0 {
			msg = msg[i+2+j+2:]
		}
	}
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return strings.TrimSpace(msg)
}

func detectSpamPhrases(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, sc := range emailSteps(s) {
		haystack := sc.view.subject + " " + sc.view.options
		hits := []string{}
		for _, p := range spamTriggerPatterns {
			if p.re.MatchString(haystack) {
				hits = append(hits, p.phrase)
			}
		}
		// One borderline phrase is not a finding. Two or more is a pattern.
		if len(hits) < 2 {
			continue
		}

		out = append(out, Finding{
			Key:         "copy_spam_phrases",
			GroupTitle:  "{count} steps read like bulk mail",
			Category:    models.AdvisorCategoryCopy,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "step",
			EntityID:    ref(sc.step.ID),
			EntityLabel: fmt.Sprintf("%s / %s", sc.campaign, stepLabel(sc)),
			ParentType:  "campaign",
			ParentID:    ref(sc.step.CampaignID),
			Impact:      clampImpact(30 + len(hits)*5),
			Title:       fmt.Sprintf("%s reads like bulk mail", stepLabel(sc)),
			Detail: fmt.Sprintf(
				"%s uses %s. Filters weight these phrases, but the bigger cost is that they make the message read as something nobody wrote to anyone in particular.",
				stepLabel(sc), joinQuoted(hits)),
			Remedy: "Rewrite those lines in the words you would use in a one-to-one email to this person. If a phrase would be strange to say out loud, it is strange to read.",
			Evidence: map[string]any{
				"campaign": sc.campaign,
				"step":     stepLabel(sc),
				"phrases":  hits,
				"subject":  sc.view.subject,
			},
		})
	}
	return out
}

func detectTooManyLinks(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, sc := range emailSteps(s) {
		// Only the first email matters here: a later step linking to a case
		// study is normal, a first touch with five links is not.
		if !sc.first {
			continue
		}
		links := sc.view.links
		if links <= maxLinksInBody {
			continue
		}

		out = append(out, Finding{
			Key:         "copy_too_many_links",
			GroupTitle:  "{count} first emails carry too many links",
			Category:    models.AdvisorCategoryCopy,
			Severity:    models.AdvisorLow,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "step",
			EntityID:    ref(sc.step.ID),
			EntityLabel: fmt.Sprintf("%s / %s", sc.campaign, stepLabel(sc)),
			ParentType:  "campaign",
			ParentID:    ref(sc.step.CampaignID),
			Impact:      clampImpact(20 + links*3),
			Title:       fmt.Sprintf("The first email in %s has %d links", sc.campaign, links),
			Detail: fmt.Sprintf(
				"%s carries %d links in a first-touch email. Filters treat link-heavy first contact as a bulk signal, and a reader with four things to click does none of them.",
				stepLabel(sc), links),
			Remedy: "Keep one link, or none. The goal of a first email is a reply, not a click.",
			Evidence: map[string]any{
				"campaign":    sc.campaign,
				"step":        stepLabel(sc),
				"link_count":  links,
				"recommended": 1,
			},
		})
	}
	return out
}

func detectShoutySubject(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, sc := range emailSteps(s) {
		subject := sc.view.subject
		if subject == "" {
			continue
		}
		letters, upper := 0, 0
		for _, r := range subject {
			if unicode.IsLetter(r) {
				letters++
				if unicode.IsUpper(r) {
					upper++
				}
			}
		}
		shouting := letters >= 8 && float64(upper)/float64(letters) > 0.7
		bangs := strings.Count(subject, "!") >= 2
		if !shouting && !bangs {
			continue
		}

		reason := "is in capitals"
		if bangs && !shouting {
			reason = "is stacked with exclamation marks"
		} else if bangs {
			reason = "is in capitals with exclamation marks"
		}

		out = append(out, Finding{
			Key:         "copy_shouty_subject",
			GroupTitle:  "{count} subject lines shout",
			Category:    models.AdvisorCategoryCopy,
			Severity:    models.AdvisorLow,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "step",
			EntityID:    ref(sc.step.ID),
			EntityLabel: fmt.Sprintf("%s / %s", sc.campaign, stepLabel(sc)),
			ParentType:  "campaign",
			ParentID:    ref(sc.step.CampaignID),
			Impact:      25,
			Title:       fmt.Sprintf("The subject line in %s %s", stepLabel(sc), reason),
			Detail: fmt.Sprintf(
				"The subject %q %s. Filters weight this as a bulk-mail marker, and a person reads it as shouting from a stranger.",
				subject, reason),
			Remedy: "Write it the way you would write to one person: sentence case, no exclamation marks.",
			Evidence: map[string]any{
				"campaign": sc.campaign,
				"step":     stepLabel(sc),
				"subject":  subject,
			},
		})
	}
	return out
}

// joinQuoted renders a short list of literals as "a", "b" and "c".
func joinQuoted(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, it := range items {
		quoted = append(quoted, fmt.Sprintf("%q", it))
	}
	return joinWords(quoted)
}
