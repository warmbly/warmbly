package tasks

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplfuncs"
	"github.com/warmbly/warmbly/internal/pkg/warmpersona"
	"github.com/warmbly/warmbly/internal/repository"
)

// Conversation represents a warmup conversation for AI generation
type Conversation struct {
	ID          uuid.UUID
	Theme       string
	Description string
	Messages    []string
}

// TemplateVariables contains variables for template rendering
type TemplateVariables struct {
	FirstName string
	LastName  string
	Email     string
	Company   string
	Phone     string
	Custom    map[string]string
}

// templateAction matches a single {{ ... }} action (no nested braces).
var templateAction = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// spacedFieldRefAt matches, anchored at the start of the slice, a dotted field
// reference whose key contains an internal space or dash (e.g. ".job title" or
// ".first-name"). Such a key is not a valid Go-template identifier, so it cannot
// be written as a `.Selector`. We rewrite it to the equivalent `(index . "key")`,
// which IS valid everywhere — standalone, inside {{if}}, and inside eq/and/... —
// giving custom fields with spaces full Go-template support.
var spacedFieldRefAt = regexp.MustCompile(`^\.[A-Za-z0-9_]+(?:[ \-]+[A-Za-z0-9_]+)+`)

// tmplCache caches parsed templates keyed by the raw template string. A stored
// nil *template.Template is a "known-bad" sentinel: that body failed to parse,
// so future renders skip straight to the naive fallback instead of re-parsing
// on every recipient. *template.Template is safe for concurrent Execute once
// parsed, so a single cached instance is reused across the whole send loop.
var tmplCache sync.Map // map[string]*template.Template ; nil value = known-bad

// buildTemplateData flattens the contact into the single map[string]string root
// the template engine executes against. Standard fields use their established
// dot-names so {{.FirstName}} keeps working; custom fields are merged in, with
// standard fields winning a name collision.
func buildTemplateData(contact models.Contact) map[string]string {
	data := make(map[string]string, len(contact.CustomFields)+5)
	for k, v := range contact.CustomFields {
		data[k] = v
	}
	data["FirstName"] = contact.FirstName
	data["LastName"] = contact.LastName
	data["Email"] = contact.Email
	data["Company"] = contact.Company
	data["Phone"] = contact.Phone
	return data
}

// rewriteSpacedFieldRefs rewrites a dotted custom-field reference whose key has
// a space or dash (`.job title`, `.first-name`) into `(index . "key")` inside
// each {{ }} action, so those fields work EVERYWHERE — standalone, inside
// {{if}}, and inside eq/and/printf/... — not just as a literal substitution.
// Plain identifier selectors ({{.FirstName}}) and any text outside an action are
// left untouched, and quoted string literals inside an action are skipped so a
// value like ".NET" is never mangled. No data is needed: the rewrite is purely
// syntactic, so it works identically at render time and at validation time.
func rewriteSpacedFieldRefs(tmpl string) string {
	if !strings.Contains(tmpl, "{{") {
		return tmpl
	}
	return templateAction.ReplaceAllStringFunc(tmpl, rewriteSpacedInAction)
}

func rewriteSpacedInAction(action string) string {
	var b strings.Builder
	b.Grow(len(action))
	var quote byte // 0 = not in a string literal; '"' or '`' otherwise
	for i := 0; i < len(action); {
		c := action[i]
		switch {
		case quote != 0:
			b.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(action) {
				b.WriteByte(action[i+1]) // keep an escaped char verbatim
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
		case c == '"' || c == '`':
			quote = c
			b.WriteByte(c)
			i++
		case c == '.':
			if m := spacedFieldRefAt.FindString(action[i:]); m != "" {
				b.WriteString(`(index . "` + m[1:] + `")`)
				i += len(m)
				continue
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// compiledTemplate returns a parsed, cached template for tmpl, or nil if the
// body is known-bad (caller falls back to naiveRenderTemplate). missingkey=zero
// makes absent map keys render as "" and test false in {{if .X}}. text/template
// (not html/template) performs no escaping, so the author's HTML body is emitted
// verbatim.
func compiledTemplate(tmpl string) *template.Template {
	if v, ok := tmplCache.Load(tmpl); ok {
		t, _ := v.(*template.Template)
		return t // may be nil (known-bad)
	}
	t, err := template.New("body").Funcs(tmplfuncs.FuncMap()).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		tmplCache.Store(tmpl, (*template.Template)(nil))
		return nil
	}
	tmplCache.Store(tmpl, t)
	return t
}

// TemplateError returns a parse error when a template's control syntax is
// malformed (e.g. an {{if}} with no {{end}}, or a bad {{eq}}), or nil when it is
// valid. Spaced/dashed custom-field references are rewritten to their `index`
// form first (exactly as the renderer does), so a valid template that uses them
// inside {{if}} does not false-fail validation. Used to block starting a
// campaign with a template that would degrade to literal {{if}} text on send.
func TemplateError(tmpl string) error {
	if tmpl == "" {
		return nil
	}
	_, err := template.New("validate").Funcs(tmplfuncs.FuncMap()).Option("missingkey=zero").Parse(rewriteSpacedFieldRefs(tmpl))
	return err
}

// RenderTemplate renders a sequence template against a contact, supporting Go
// text/template conditionals ({{if}}/{{else}}/{{eq}}), standard variables, and
// custom fields. It NEVER hard-fails: any parse or execution error falls back to
// the naive replacement path so a send always produces a body. Spintax is
// intentionally left untouched here (single-brace {a|b} survives the template
// pass) and expanded later in the pipeline where applicable.
func RenderTemplate(tmpl string, contact models.Contact) string {
	if tmpl == "" {
		return tmpl
	}

	data := buildTemplateData(contact)
	prepared := rewriteSpacedFieldRefs(tmpl)

	t := compiledTemplate(prepared)
	if t == nil {
		return naiveRenderTemplate(tmpl, contact) // known-bad -> legacy path
	}

	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return naiveRenderTemplate(tmpl, contact)
	}
	return b.String()
}

// naiveRenderTemplate is the legacy renderer: a literal {{.Key}} -> value
// substitution for the standard fields and every custom field. It is the
// graceful fallback when text/template parsing or execution fails, so a body
// always renders even for malformed conditional syntax.
func naiveRenderTemplate(tmpl string, contact models.Contact) string {
	result := tmpl
	result = strings.ReplaceAll(result, "{{.FirstName}}", contact.FirstName)
	result = strings.ReplaceAll(result, "{{.LastName}}", contact.LastName)
	result = strings.ReplaceAll(result, "{{.Email}}", contact.Email)
	result = strings.ReplaceAll(result, "{{.Company}}", contact.Company)
	result = strings.ReplaceAll(result, "{{.Phone}}", contact.Phone)
	for k, v := range contact.CustomFields {
		result = strings.ReplaceAll(result, fmt.Sprintf("{{.%s}}", k), v)
	}
	return result
}

// TemplatePreview is the result of rendering a campaign template against one
// contact, plus any problems found — used by the composer's live preview and
// inline validation so a broken {{if}} or a token that won't resolve is caught
// before launch instead of shipping literally.
type TemplatePreview struct {
	Subject    string   `json:"subject"`
	BodyHTML   string   `json:"body_html"`
	BodyPlain  string   `json:"body_plain"`
	Errors     []string `json:"errors,omitempty"`     // template parse errors (these block sending)
	Unresolved []string `json:"unresolved,omitempty"` // literal {{…}} tokens left after render
}

// unresolvedToken matches a {{…}} token still present after rendering (i.e. one
// that failed to parse and fell through to literal substitution).
var unresolvedToken = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// PreviewTemplates renders subject/html/plain against contact EXACTLY as the
// send path does (template render + spintax), and reports parse errors plus any
// tokens that did not resolve.
func PreviewTemplates(subject, bodyHTML, bodyPlain string, contact models.Contact) TemplatePreview {
	p := TemplatePreview{
		Subject:   expandSpintax(RenderTemplate(subject, contact)),
		BodyHTML:  expandSpintax(RenderTemplate(bodyHTML, contact)),
		BodyPlain: expandSpintax(RenderTemplate(bodyPlain, contact)),
	}
	for _, f := range []struct{ name, raw string }{{"subject", subject}, {"body", bodyHTML}, {"plain text", bodyPlain}} {
		if err := TemplateError(f.raw); err != nil {
			p.Errors = append(p.Errors, f.name+": "+err.Error())
		}
	}
	seen := map[string]bool{}
	for _, out := range []string{p.Subject, p.BodyHTML, p.BodyPlain} {
		for _, tok := range unresolvedToken.FindAllString(out, -1) {
			if !seen[tok] {
				seen[tok] = true
				p.Unresolved = append(p.Unresolved, tok)
			}
		}
	}
	return p
}

// AddSignature adds signature to email body
func AddSignature(body string, signature string, isHTML bool) string {
	if signature == "" {
		return body
	}

	if isHTML {
		return body + "<br><br>" + signature
	}

	return body + "\n\n" + signature
}

// AddOpenTrackingPixel adds an invisible tracking pixel to HTML email
// The pixel URL points to the Rust tracking service endpoint: /t/o/{taskID}.png
func AddOpenTrackingPixel(htmlBody string, taskID uuid.UUID, trackingDomain string) string {
	if trackingDomain == "" {
		trackingDomain = "track.warmbly.com"
	}

	// Use /t/o/ path to match Rust tracking service
	pixelURL := fmt.Sprintf("https://%s/t/o/%s.png", trackingDomain, taskID.String())
	pixel := fmt.Sprintf(`<img src="%s" width="1" height="1" style="display:none;" alt="" />`, pixelURL)

	// Try to insert before closing body tag
	if strings.Contains(htmlBody, "</body>") {
		return strings.Replace(htmlBody, "</body>", pixel+"</body>", 1)
	}

	// Otherwise append to end
	return htmlBody + pixel
}

// WrapLinksForTracking rewrites every external link to an opaque
// click-tracking ticket (https://<domain>/c/<id>) and returns the minted
// rows. The destination never travels inside the link, so there is nothing
// to forge: the tracking service resolves tickets via the backend internal
// API and 404s anything it does not know. The caller MUST persist the
// returned rows before using the rewritten body (and fall back to the
// original body on failure) so an email can never ship dead tickets.
func WrapLinksForTracking(htmlBody string, taskID, campaignID uuid.UUID, trackingDomain string) (string, []repository.TrackedLink) {
	if trackingDomain == "" {
		trackingDomain = "track.warmbly.com"
	}

	// Regex to find href attributes
	linkRegex := regexp.MustCompile(`href="([^"]+)"`)
	var links []repository.TrackedLink

	result := linkRegex.ReplaceAllStringFunc(htmlBody, func(match string) string {
		// Extract the original URL
		originalURL := linkRegex.FindStringSubmatch(match)[1]

		// Skip if already a tracking link or anchor link
		if strings.HasPrefix(originalURL, "#") ||
			strings.Contains(originalURL, trackingDomain) ||
			strings.HasPrefix(originalURL, "mailto:") ||
			strings.HasPrefix(originalURL, "tel:") {
			return match
		}

		// Skip data URLs and javascript links
		if strings.HasPrefix(originalURL, "data:") ||
			strings.HasPrefix(originalURL, "javascript:") {
			return match
		}

		// Only http(s) destinations are storable redirect targets
		if !strings.HasPrefix(originalURL, "http://") && !strings.HasPrefix(originalURL, "https://") {
			return match
		}

		id := uuid.New()
		links = append(links, repository.TrackedLink{
			ID:          id,
			TaskID:      taskID,
			CampaignID:  campaignID,
			Destination: originalURL,
		})

		return fmt.Sprintf(`href="https://%s/c/%s"`, trackingDomain, id.String())
	})

	return result, links
}

// personaPick chooses from a mailbox's preferred subset of phrasing options so
// each mailbox keeps a consistent "voice" while still varying message to
// message. Falls back gracefully for tiny option sets.
func personaPick(p warmpersona.Persona, axis string, opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	k := 3
	if k > len(opts) {
		k = len(opts)
	}
	subset := p.Subset(axis, len(opts), k)
	if len(subset) == 0 {
		return opts[rand.Intn(len(opts))]
	}
	return opts[subset[rand.Intn(len(subset))]]
}

// GenerateConversationReplyEmail renders one ordered reply turn. Turn 1 maps
// to Messages[0]; exhausted threads return false instead of repeating a line.
func GenerateConversationReplyEmail(conversation Conversation, account models.Email, turn int) (string, bool) {
	if turn < 1 || turn > len(conversation.Messages) {
		return "", false
	}
	body := spinClean(conversation.Messages[turn-1])
	if body == "" {
		return "", false
	}

	signature := account.Name
	if signature == "" {
		signature = account.Email
	}

	persona := warmpersona.For(account.ID)
	signoffs := []string{"Best regards,", "Best,", "Cheers,", "Thanks,", "Talk soon,", "All the best,", "Speak soon,", "Take care,"}
	signoff := personaPick(persona, "signoff", signoffs)

	return body + "\n\n" + signoff + "\n" + signature, true
}

// GenerateConversationOpeningEmail renders an AI opening without consuming a
// pre-generated reply turn.
func GenerateConversationOpeningEmail(conversation Conversation, account models.Email) string {
	return generateConversationOpeningEmail(conversation, account, false)
}

// GenerateConversationEmail retains the reviewed static-library renderer.
func GenerateConversationEmail(conversation Conversation, account models.Email, isReply bool) string {
	if isReply {
		if body, ok := GenerateConversationReplyEmail(conversation, account, 1); ok {
			return body
		}
	}
	return generateConversationOpeningEmail(conversation, account, true)
}

// generateConversationOpeningEmail renders a plaintext opening. Reviewed
// static content can append one question; AI openings keep all ordered reply
// turns unused for the later back-and-forth.
func generateConversationOpeningEmail(conversation Conversation, account models.Email, includeQuestion bool) string {
	signature := account.Name
	if signature == "" {
		signature = account.Email
	}

	persona := warmpersona.For(account.ID)

	greetings := []string{"Hi,", "Hey,", "Hi there,", "Hello,", "Hey there,", "Morning,", "Hello there,"}
	signoffs := []string{"Best regards,", "Best,", "Cheers,", "Thanks,", "Talk soon,", "All the best,", "Speak soon,", "Take care,"}

	greeting := personaPick(persona, "greeting", greetings)
	signoff := personaPick(persona, "signoff", signoffs)

	pickMessage := func() string {
		if len(conversation.Messages) == 0 {
			return ""
		}
		return spinClean(conversation.Messages[rand.Intn(len(conversation.Messages))])
	}

	description := spinClean(conversation.Description)
	if description == "" {
		description = "Just wanted to check in with a quick note."
	}

	openers := []string{
		"Hope your week is going well.",
		"Hope you're doing well.",
		"Hope things are good on your end.",
		"Hope you've had a good start to the week.",
		"Hope all's well with you.",
	}

	var sb strings.Builder
	sb.WriteString(greeting)
	sb.WriteString("\n\n")
	// ~60% include a short opener for a warmer, less terse message.
	if rand.Float64() < 0.6 {
		sb.WriteString(openers[rand.Intn(len(openers))])
		sb.WriteString(" ")
	}
	sb.WriteString(description)

	if includeQuestion {
		if question := pickMessage(); question != "" {
			sb.WriteString("\n\n")
			sb.WriteString(question)
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(signoff)
	sb.WriteString("\n")
	sb.WriteString(signature)
	return sb.String()
}

// ExtractPlainTextFromHTML converts HTML to plain text (basic implementation)
func ExtractPlainTextFromHTML(html string) string {
	// Remove HTML tags
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	text := tagRegex.ReplaceAllString(html, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")

	// Remove extra whitespace
	text = strings.TrimSpace(text)
	multiSpaceRegex := regexp.MustCompile(`\s+`)
	text = multiSpaceRegex.ReplaceAllString(text, " ")

	return text
}
