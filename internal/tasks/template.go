package tasks

import (
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/warmpersona"
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

// identifierKey matches a Go-template-safe selector key: a leading letter or
// underscore followed by letters, digits, or underscores. Only keys matching
// this can be referenced via the {{.Key}} selector syntax; non-identifier
// custom-field keys (e.g. "job title", "first-name") are substituted literally
// by a pre-pass before the template engine parses the body.
var identifierKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// legacyDotToken matches a single {{.<anything-but-brace>}} token. Used by the
// pre-pass to find tokens whose key is a known but non-identifier custom field.
var legacyDotToken = regexp.MustCompile(`\{\{\.([^{}]+)\}\}`)

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

// rewriteNonIdentifierTokens substitutes {{.<key>}} tokens whose key exists in
// data but is NOT a valid Go-template identifier (so the selector syntax can't
// reference it, e.g. "job title"). Identifier tokens are left for the engine so
// they remain usable inside {{if}}/{{eq}}.
func rewriteNonIdentifierTokens(tmpl string, data map[string]string) string {
	if !strings.Contains(tmpl, "{{.") {
		return tmpl
	}
	return legacyDotToken.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimSpace(legacyDotToken.FindStringSubmatch(match)[1])
		if identifierKey.MatchString(key) {
			return match // engine resolves it (and it may be used in if/eq)
		}
		if v, ok := data[key]; ok {
			return v // legacy literal substitution for non-identifier keys
		}
		return match // unknown + non-identifier: leave for engine/missingkey
	})
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
	t, err := template.New("body").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		tmplCache.Store(tmpl, (*template.Template)(nil))
		return nil
	}
	tmplCache.Store(tmpl, t)
	return t
}

// TemplateError returns a parse error when a template's control syntax is
// malformed (e.g. an {{if}} with no {{end}}, or a bad {{eq}}), or nil when it is
// valid. Non-identifier {{.key}} tokens (custom fields with spaces) are
// neutralized first — the renderer substitutes those per contact, so they must
// not false-fail validation. Used to block starting a campaign with a template
// that would otherwise degrade to literal {{if}} text in the sent email.
func TemplateError(tmpl string) error {
	if tmpl == "" {
		return nil
	}
	prepared := legacyDotToken.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimSpace(legacyDotToken.FindStringSubmatch(match)[1])
		if identifierKey.MatchString(key) {
			return match
		}
		return "" // non-identifier custom key: substituted per contact at render
	})
	_, err := template.New("validate").Option("missingkey=zero").Parse(prepared)
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
	prepared := rewriteNonIdentifierTokens(tmpl, data)

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

// WrapLinksForTracking wraps all links in HTML for click tracking
// The tracking URL points to the Rust tracking service endpoint: /t/c/{taskID}?url={original_url}
func WrapLinksForTracking(htmlBody string, taskID uuid.UUID, trackingDomain string) string {
	if trackingDomain == "" {
		trackingDomain = "track.warmbly.com"
	}

	// Regex to find href attributes
	linkRegex := regexp.MustCompile(`href="([^"]+)"`)

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

		// Use /t/c/ path to match Rust tracking service
		// URL encode the original URL properly
		trackingURL := fmt.Sprintf("https://%s/t/c/%s?url=%s",
			trackingDomain,
			taskID.String(),
			url.QueryEscape(originalURL))

		return fmt.Sprintf(`href="%s"`, trackingURL)
	})

	return result
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

// GenerateConversationEmail renders a plaintext warmup body from a conversation.
//
// Bodies vary three ways to avoid the small-fixed-corpus fingerprint: a
// per-mailbox persona biases the greeting/sign-off "voice", {a|b|c} spintax in
// the description/messages is expanded per send, and the structure (optional
// opener line, optional follow-up question) is randomised. Plaintext only — no
// links, no HTML, in line with warmup policy.
func GenerateConversationEmail(conversation Conversation, account models.Email, isReply bool) string {
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

	if isReply {
		replyStarters := []string{
			"Thanks for your message.",
			"Appreciate you getting back to me.",
			"Good to hear from you.",
			"Thanks for the reply.",
			"Great to hear back.",
			"Thanks for the note — good timing.",
		}
		lead := replyStarters[rand.Intn(len(replyStarters))]
		question := pickMessage()

		var sb strings.Builder
		sb.WriteString(lead)
		if question != "" {
			sb.WriteString("\n\n")
			sb.WriteString(question)
		}
		sb.WriteString("\n\n")
		sb.WriteString(signoff)
		sb.WriteString("\n")
		sb.WriteString(signature)
		return sb.String()
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

	if question := pickMessage(); question != "" {
		sb.WriteString("\n\n")
		sb.WriteString(question)
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
