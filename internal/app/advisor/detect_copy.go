package advisor

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

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
			Run:      detectBrokenTemplate,
		},
		{
			Key:      "copy_spam_phrases",
			Category: models.AdvisorCategoryCopy,
			About:    "Phrases that both spam filters and human readers treat as bulk-mail markers. The filter cost is real but secondary; the main cost is that these phrases make a message read as something nobody wrote to anyone in particular.",
			Run:      detectSpamPhrases,
		},
		{
			Key:      "copy_too_long",
			Category: models.AdvisorCategoryCopy,
			About:    "Cold emails long enough that a busy recipient will not read them. Length is the most reliable predictor of a cold email being ignored on a phone, which is where most of them are opened.",
			Run:      detectCopyTooLong,
		},
		{
			Key:      "copy_subject_too_long",
			Category: models.AdvisorCategoryCopy,
			About:    "Subject lines long enough to be truncated in the inbox list, especially on mobile clients, so the part that would earn the open is never seen.",
			Run:      detectSubjectTooLong,
		},
		{
			Key:      "copy_too_many_links",
			Category: models.AdvisorCategoryCopy,
			About:    "Link count in a cold email. Multiple links in a first-touch email is a strong bulk-mail signal to filters, and it splits the reader's attention away from the single thing you want them to do.",
			Run:      detectTooManyLinks,
		},
		{
			Key:      "copy_shouty_subject",
			Category: models.AdvisorCategoryCopy,
			About:    "Subject lines in capitals or stacked with exclamation marks. This is a bulk-mail signal for filters and reads as shouting to a person.",
			Run:      detectShoutySubject,
		},
	}
}

var (
	// linkRe counts hyperlinks in either body form.
	linkRe = regexp.MustCompile(`(?i)https?://[^\s"'<>)]+|<a\s+[^>]*href=`)
	// wordRe splits on whitespace for a word count that does not need to be
	// exact, only stable.
	wordRe = regexp.MustCompile(`\S+`)
	// tagRe strips HTML so the length check measures prose, not markup.
	tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
	// styleRe removes script/style blocks before stripping tags, so their
	// contents do not count as words.
	styleRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
)

// stepContext pairs a step with its campaign for labelling.
type stepContext struct {
	step     repository.AdvisorStep
	campaign string
	status   string
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
	for _, st := range s.Steps {
		if st.Kind != "email" {
			continue
		}
		status := statuses[st.CampaignID.String()]
		if status == "draft" || status == "" {
			continue
		}
		out = append(out, stepContext{step: st, campaign: names[st.CampaignID.String()], status: status})
	}
	return out
}

// stepLabel is the human name for a step in a finding.
func stepLabel(sc stepContext) string {
	name := sc.step.Name
	if name == "" {
		name = fmt.Sprintf("Step %d", sc.step.Position+1)
	}
	return name
}

// bodyText returns the step's prose: the plaintext body when present,
// otherwise the HTML body with markup stripped.
func bodyText(st repository.AdvisorStep) string {
	if strings.TrimSpace(st.BodyPlain) != "" {
		return st.BodyPlain
	}
	stripped := styleRe.ReplaceAllString(st.BodyHTML, " ")
	return tagRe.ReplaceAllString(stripped, " ")
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
			"subject": sc.step.Subject,
			"body":    sc.step.BodyPlain,
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
		haystack := strings.ToLower(sc.step.Subject + " " + bodyText(sc.step))
		hits := []string{}
		for _, phrase := range spamTriggerPhrases {
			if strings.Contains(haystack, phrase) {
				hits = append(hits, phrase)
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
				"subject":  sc.step.Subject,
			},
		})
	}
	return out
}

func detectCopyTooLong(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, sc := range emailSteps(s) {
		words := len(wordRe.FindAllString(bodyText(sc.step), -1))
		if words <= bodyTooLongWords {
			continue
		}

		out = append(out, Finding{
			Key:         "copy_too_long",
			GroupTitle:  "{count} emails are too long to get read",
			Category:    models.AdvisorCategoryCopy,
			Severity:    models.AdvisorLow,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "step",
			EntityID:    ref(sc.step.ID),
			EntityLabel: fmt.Sprintf("%s / %s", sc.campaign, stepLabel(sc)),
			ParentType:  "campaign",
			ParentID:    ref(sc.step.CampaignID),
			Impact:      clampImpact(15 + (words-bodyTooLongWords)/20),
			Title:       fmt.Sprintf("%s is %d words long", stepLabel(sc), words),
			Detail: fmt.Sprintf(
				"%s runs to %d words. Most cold email is opened on a phone, where anything past a screen and a half is scrolled past rather than read.",
				stepLabel(sc), words),
			Remedy: "Cut it to under 120 words: one line on why you are writing to this person specifically, one line on what you do, one question. Everything else belongs in the reply.",
			Evidence: map[string]any{
				"campaign":    sc.campaign,
				"step":        stepLabel(sc),
				"word_count":  words,
				"recommended": 120,
				"step_sends":  sc.step.Sent,
			},
		})
	}
	return out
}

func detectSubjectTooLong(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, sc := range emailSteps(s) {
		subject := strings.TrimSpace(sc.step.Subject)
		if len(subject) <= subjectTooLong {
			continue
		}

		out = append(out, Finding{
			Key:         "copy_subject_too_long",
			GroupTitle:  "{count} subject lines get cut off in the inbox",
			Category:    models.AdvisorCategoryCopy,
			Severity:    models.AdvisorLow,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "step",
			EntityID:    ref(sc.step.ID),
			EntityLabel: fmt.Sprintf("%s / %s", sc.campaign, stepLabel(sc)),
			ParentType:  "campaign",
			ParentID:    ref(sc.step.CampaignID),
			Impact:      clampImpact(15 + (len(subject)-subjectTooLong)/5),
			Title:       fmt.Sprintf("The subject line in %s gets cut off", stepLabel(sc)),
			Detail: fmt.Sprintf(
				"The subject is %d characters. Mobile inbox lists show roughly the first %d, so the part that would earn the open is never seen.",
				len(subject), subjectTooLong-15),
			Remedy: "Get it under 45 characters. Short, specific, and lowercase reads like a colleague; long and title-cased reads like a newsletter.",
			Evidence: map[string]any{
				"campaign":    sc.campaign,
				"step":        stepLabel(sc),
				"subject":     subject,
				"length":      len(subject),
				"recommended": 45,
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
		if sc.step.Position != 0 {
			continue
		}
		// One representation only: the plain and HTML bodies carry the same
		// links, so concatenating them double-counts every one and turns a
		// two-link limit into a one-link limit.
		body := sc.step.BodyPlain
		if strings.TrimSpace(body) == "" {
			body = sc.step.BodyHTML
		}
		links := len(linkRe.FindAllString(body, -1))
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
		subject := strings.TrimSpace(sc.step.Subject)
		if subject == "" {
			continue
		}
		letters, upper := 0, 0
		for _, r := range subject {
			if r >= 'a' && r <= 'z' {
				letters++
			}
			if r >= 'A' && r <= 'Z' {
				letters++
				upper++
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
