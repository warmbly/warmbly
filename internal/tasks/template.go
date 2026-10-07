package tasks

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/pkg/tmplfuncs"
	"github.com/warmbly/warmbly/internal/pkg/warmpersona"
)

// Conversation represents a warmup conversation for AI generation
type Conversation struct {
	ID          uuid.UUID
	Version     string
	Subject     string
	Theme       string
	Description string
	Messages    []string
}

// TemplateContext holds typed per-send values, separate from contact fields.
type TemplateContext struct {
	Sender          TemplateSender
	UnsubscribeLink string
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
// nil is a "known-bad" sentinel: that body failed to compile, so future renders
// skip straight to the naive fallback instead of re-parsing on every recipient.
// *template.Template is safe for concurrent Execute once parsed, so a single
// cached instance is reused across the whole send loop.
var tmplCache tmplfuncs.Cache

// Standard fields and the typed Sender namespace take precedence over custom fields.
func buildTemplateData(contact models.Contact, context TemplateContext) map[string]any {
	data := make(map[string]any, len(contact.CustomFields)+7)
	for k, v := range contact.CustomFields {
		data[k] = v
	}
	data["FirstName"] = contact.FirstName
	data["LastName"] = contact.LastName
	data["Email"] = contact.Email
	data["Company"] = contact.Company
	data["Phone"] = contact.Phone
	data["Sender"] = context.Sender
	data[UnsubscribeLinkVar] = context.UnsubscribeLink
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
			// A nested struct selector must not consume the following function argument.
			fieldStart := i == 0 || strings.ContainsRune("{( \t\r\n", rune(action[i-1]))
			if m := spacedFieldRefAt.FindString(action[i:]); fieldStart && m != "" {
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
// body is known-bad (caller falls back to standalone actions). missingkey=zero
// makes absent map keys test false in {{if .X}}. text/template
// (not html/template) performs no escaping, so the author's HTML body is emitted
// verbatim.
func compiledTemplate(tmpl string) *template.Template {
	if t, ok := tmplCache.Load(tmpl); ok {
		return t // may be nil (known-bad)
	}
	t, err := tmplfuncs.Compile("body", tmpl)
	if err != nil {
		tmplCache.Store(tmpl, nil)
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
	_, err := tmplfuncs.Compile("validate", rewriteSpacedFieldRefs(tmpl))
	return err
}

// RenderTemplate renders a sequence template against a contact, supporting Go
// text/template conditionals ({{if}}/{{else}}/{{eq}}), standard variables, and
// custom fields. It NEVER hard-fails: any parse or execution error falls back to
// standalone actions so a send always produces a body. Spintax is
// intentionally left untouched here (single-brace {a|b} survives the template
// pass) and expanded later in the pipeline where applicable.
func RenderTemplate(tmpl string, contact models.Contact) string {
	return RenderTemplateWith(tmpl, contact, TemplateContext{})
}

// RenderTemplateWith uses Go's native struct access for the sending mailbox.
func RenderTemplateWith(tmpl string, contact models.Contact, context TemplateContext) string {
	if tmpl == "" {
		return tmpl
	}

	data := buildTemplateData(contact, context)
	prepared := rewriteSpacedFieldRefs(tmpl)

	t := compiledTemplate(prepared)
	if t == nil {
		return fallbackRenderTemplate(tmpl, data)
	}

	out, err := tmplfuncs.Execute(t, data)
	if err != nil {
		return fallbackRenderTemplate(tmpl, data)
	}
	return strings.ReplaceAll(out, "<no value>", "")
}

// Recover standalone actions with the same engine, leaving broken control syntax literal.
func fallbackRenderTemplate(tmpl string, data map[string]any) string {
	return templateAction.ReplaceAllStringFunc(tmpl, func(action string) string {
		t := compiledTemplate(rewriteSpacedFieldRefs(action))
		if t == nil {
			return action
		}
		out, err := tmplfuncs.Execute(t, data)
		if err != nil {
			return action
		}
		return strings.ReplaceAll(out, "<no value>", "")
	})
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
	// HTMLFindings is what mail clients will do to this body: what they strip,
	// what CSS they ignore, and whether Gmail will clip it. Advisory only.
	HTMLFindings []mailhtml.Finding `json:"html_findings,omitempty"`
}

// PreviewUnsubscribeLink stands in for the per-recipient link in previews.
// The same width as a real one, so a plain-text preview (where the address is
// printed in full) reads as wide as the send will.
const PreviewUnsubscribeLink = "https://example.com" + unsublink.Path + unsublink.ExampleTicket

// unresolvedToken matches a {{…}} token still present after rendering (i.e. one
// that failed to parse and fell through to literal substitution).
var unresolvedToken = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// PreviewTemplates renders subject/html/plain against contact EXACTLY as the
// send path does (template render + spintax), and reports parse errors plus any
// tokens that did not resolve.
func PreviewTemplates(subject, bodyHTML, bodyPlain string, contact models.Contact) TemplatePreview {
	return previewTemplatesWith(subject, bodyHTML, bodyPlain, contact, TemplateContext{UnsubscribeLink: PreviewUnsubscribeLink})
}

// previewTemplatesWith renders with the same sender context as a real send.
func previewTemplatesWith(subject, bodyHTML, bodyPlain string, contact models.Contact, context TemplateContext) TemplatePreview {
	p := TemplatePreview{
		Subject:   expandSpintax(RenderTemplateWith(subject, contact, context)),
		BodyHTML:  expandSpintax(RenderTemplateWith(bodyHTML, contact, context)),
		BodyPlain: expandSpintax(RenderTemplateWith(bodyPlain, contact, context)),
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

// AddSignature places the mailbox signature under the body. HTML gets its own
// block with a top margin, not <br><br>: the breaks stacked against the body's
// own trailing margin and showed as blank lines in Apple Mail and Outlook.
func AddSignature(body string, signature string, isHTML bool) string {
	if signature == "" {
		return body
	}

	if !isHTML {
		return body + "\n\n" + signature
	}

	block := `<div style="margin-top:16px">` + signature + `</div>`
	// Inside the container the email was laid out in, as for the opt-out
	// footer: after it, a signature lands against the left edge of the window
	// instead of under the copy it signs off (issue #462).
	return mailhtml.AppendToContent(body, block)
}

// AppendForwarded puts the message a forward carries under the note and
// signature, the order every mail client writes a forward in.
func AppendForwarded(bodyHTML, bodyPlain, forwardedHTML, forwardedPlain string) (string, string) {
	if forwardedHTML != "" {
		if strings.TrimSpace(bodyHTML) == "" {
			bodyHTML = forwardedHTML
		} else {
			bodyHTML = mailhtml.AppendToContent(bodyHTML, "<br>"+forwardedHTML)
		}
	}
	if forwardedPlain != "" {
		if note := strings.TrimRight(bodyPlain, " \t\r\n"); note == "" {
			bodyPlain = forwardedPlain
		} else {
			bodyPlain = note + "\n\n" + forwardedPlain
		}
	}
	return bodyHTML, bodyPlain
}

// AddOpenTrackingPixel adds an invisible tracking pixel to HTML email.
// The pixel URL points to the tracking service endpoint: /t/o/{taskID}.png.
//
// An empty host means this install has no tracking host at all, and the pixel
// is left out. It used to fall back to a hardcoded warmbly.com name, so a
// self-hosted deployment mailed a pixel pointing at someone else's service:
// nothing was ever recorded, and the recipient's open was reported to a third
// party instead.
func AddOpenTrackingPixel(htmlBody string, taskID uuid.UUID, trackingDomain string) string {
	pixelURL := config.TrackingURL(trackingDomain, fmt.Sprintf("/t/o/%s.png", taskID.String()))
	if pixelURL == "" {
		return htmlBody
	}
	pixel := fmt.Sprintf(`<img src="%s" width="1" height="1" style="display:none;" alt="" />`, pixelURL)
	return mailhtml.InsertBeforeBodyEnd(htmlBody, pixel)
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
	body := strings.TrimSpace(conversation.Messages[turn-1])
	if body == "" || strings.ContainsAny(body, "<>{}\x00\r") {
		return "", false
	}

	signature := account.Name
	if signature == "" {
		signature = account.Email
	}

	rendered, err := generation.RenderCanonicalTurn(body, signature)
	return rendered, err == nil
}

// GenerateConversationOpeningEmail renders an AI opening without consuming a
// pre-generated reply turn.
func GenerateConversationOpeningEmail(conversation Conversation, account models.Email) string {
	return generateConversationOpeningEmail(conversation, account)
}

// GenerateConversationEmail renders an opening or the first ordered reply.
func GenerateConversationEmail(conversation Conversation, account models.Email, isReply bool) string {
	if isReply {
		body, _ := GenerateConversationReplyEmail(conversation, account, 1)
		return body
	}
	return generateConversationOpeningEmail(conversation, account)
}

// Openings never consume a future reply or append a random question.
func generateConversationOpeningEmail(conversation Conversation, account models.Email) string {
	signature := account.Name
	if signature == "" {
		signature = account.Email
	}

	description := strings.TrimSpace(conversation.Description)
	if description == "" || strings.ContainsAny(description, "<>{}\x00\r") {
		return ""
	}
	rendered, _ := generation.RenderCanonicalTurn(description, signature)
	return rendered
}

// VettedDiagnosticConversations uses a separate immutable source-ID namespace.
func VettedDiagnosticConversations() []Conversation {
	return []Conversation{
		{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-clock:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-clock", Subject: "Simulated diagnostic: fictional clock", Description: "Simulated diagnostic. In this fictional example, the clock reads 14:00, not 15:00. Which time does the example use?", Messages: []string{"The fictional example uses 14:00, not 15:00.", "Agreed. The example is closed; no real meeting was scheduled."}},
		{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-count:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-count", Subject: "Simulated diagnostic: sample count", Description: "Simulated diagnostic. This hypothetical sample contains 3 blue cards and 2 green cards. How many cards are in the sample?", Messages: []string{"There are 5 cards in the hypothetical sample: 3 blue and 2 green.", "That matches the hypothetical sample. This scenario is complete; no physical cards were exchanged."}},
		{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-document:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-document", Subject: "Simulated diagnostic: hypothetical draft review",
			Description: "Simulated diagnostic. In this hypothetical draft, Section A lists assumptions and Section B lists unknowns. An erroneous summary says inbox placement passed, but the draft contains no measured delivery results. Which claim needs correction?",
			Messages: []string{
				"The hypothetical inbox-placement claim needs correction: neither assumptions nor unknowns establish a measured result. Should the summary instead distinguish Section A from Section B?",
				"Yes. Section A lists assumptions; Section B lists unknowns. Neither section proves inbox placement. Can we close the fictional review with that distinction?",
				"Agreed. The fictional summary distinguishes assumptions from unknowns and makes no placement claim. This review scenario is closed; no real document was reviewed or changed.",
			},
		},
		{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-summary:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-summary", Subject: "Simulated diagnostic: summary correction",
			Description: "Simulated diagnostic. A fictional checklist has 12 items: 9 marked complete and 3 marked unknown, with none marked failed. A draft incorrectly calls all 12 complete. What should the corrected summary say?",
			Messages: []string{
				"The fictional checklist has 9 complete items and 3 unknown items, not 12 complete items. Does an unknown item count as failed or passed?",
				"No. Unknown items are neither failed nor passed; the checklist has none marked failed. Should the corrected summary keep the 9 complete and 3 unknown counts separate?",
				"Yes. The corrected hypothetical summary keeps 9 complete and 3 unknown items separate, with none marked failed. The example is closed; no actual provider check was performed.",
			},
		},
		{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-timezone:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-timezone", Subject: "Simulated diagnostic: fixed-offset date note",
			Description: "Simulated diagnostic. On 2026-10-08 the fictional clock reads 09:00 UTC, or 11:00 at the fixed offset UTC+02:00. This is not a meeting invitation. Which local date and time belong in the hypothetical note?",
			Messages: []string{
				"The note should use 2026-10-08 at 11:00 with the fixed offset UTC+02:00, corresponding to 09:00 UTC. Should it keep the fixed offset rather than guess a regional timezone?",
				"Yes. Keep 2026-10-08, 09:00 UTC and 11:00 at UTC+02:00 unchanged. The example specifies no regional timezone or meeting. Can we close without creating an appointment?",
				"Agreed. The fictional date remains 2026-10-08, with 09:00 UTC equal to 11:00 at UTC+02:00. This example is closed; no appointment was created.",
			},
		},
		{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-plaintext:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-plaintext", Subject: "Simulated diagnostic: plaintext label review",
			Description: "Simulated diagnostic. This constructed plaintext example uses written labels, not color or linked instructions. Its hypothetical items are Status: unknown and Next step: review. Can the labels be understood without opening a link?",
			Messages: []string{
				"Yes. Status: unknown and Next step: review are written in the example, without a link or color requirement. Does that alone prove how an email client displays a delivered message?",
				"No. This is a locally constructed example, not a delivered-client accessibility test. Should the summary preserve the unknown status and that testing limit?",
				"Yes. The example retains Status: unknown and Next step: review. This label-review scenario is closed; no delivered-client accessibility result is claimed.",
			},
		},
		{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-conditional:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-conditional", Subject: "Simulated diagnostic: conditional wording",
			Description: "Simulated diagnostic. In a hypothetical plan, a draft may be updated only if a reviewer approves it. No approval exists, and no update is promised for 2026-10-09. What should the draft status say?",
			Messages: []string{
				"The hypothetical status is pending review: no approval exists and the update is conditional. Should the wording promise an update on 2026-10-09?",
				"No. The approval condition has not been met, so no update is promised for 2026-10-09. Can we keep pending review instead of implying an approval?",
				"Agreed. The hypothetical draft remains pending review, without approval or a promised update for 2026-10-09. This scenario is closed; no real draft was changed or commitment made.",
			},
		},
		{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-summary-hu:v1")), Version: generation.DiagnosticScenarioVersion, Theme: "diagnostic-summary-hu", Subject: "Simulated diagnostic: magyar összefoglaló",
			Description: "Simulated diagnostic. Ebben a kitalált jegyzetben 4 ellenőrzésből 3 eredménye ismeretlen, 1 pedig nincs elvégezve. Egyik sem igazolt siker. Hogyan őrizzük meg ezt a különbséget az összefoglalóban?",
			Messages: []string{
				"Az összefoglalóban 3 ismeretlen eredményt és 1 el nem végzett ellenőrzést írjunk; ne állítsunk sikert. Jelenthet-e az ismeretlen eredmény igazolt sikert?",
				"Nem. Az ismeretlen nem igazolt siker, az el nem végzett ellenőrzés pedig továbbra sincs elvégezve. Maradjon ez kitalált példa, ne szolgáltatói mérés?",
				"Igen. A kitalált példa lezárult: 3 eredmény ismeretlen, 1 ellenőrzés nincs elvégezve. Valós szolgáltatói tesztet nem végeztünk.",
			},
		},
	}
}

func ResolveDiagnosticConversation(id uuid.UUID) (Conversation, bool) {
	for _, c := range VettedDiagnosticConversations() {
		if c.ID == id {
			return c, true
		}
	}
	return Conversation{}, false
}

// ExtractPlainTextFromHTML derives the text/plain alternative that ships
// beside an HTML body.
//
// It used to strip tags with a regex, which put a designed email's whole
// stylesheet at the top of the text part and ran every paragraph into one
// line (issue #393). mailhtml renders the document instead, so blocks, lists,
// table rows and link destinations survive.
func ExtractPlainTextFromHTML(body string) string {
	return mailhtml.ToPlainText(body)
}
