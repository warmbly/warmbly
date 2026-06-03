package tasks

import (
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"

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

// RenderTemplate renders a template string with contact variables
func RenderTemplate(template string, contact models.Contact) string {
	vars := TemplateVariables{
		FirstName: contact.FirstName,
		LastName:  contact.LastName,
		Email:     contact.Email,
		Company:   contact.Company,
		Phone:     contact.Phone,
		Custom:    make(map[string]string),
	}

	// Parse custom fields if present
	if contact.CustomFields != nil {
		vars.Custom = contact.CustomFields
	}

	result := template

	// Replace standard variables
	result = strings.ReplaceAll(result, "{{.FirstName}}", vars.FirstName)
	result = strings.ReplaceAll(result, "{{.LastName}}", vars.LastName)
	result = strings.ReplaceAll(result, "{{.Email}}", vars.Email)
	result = strings.ReplaceAll(result, "{{.Company}}", vars.Company)
	result = strings.ReplaceAll(result, "{{.Phone}}", vars.Phone)

	// Replace custom variables
	for k, v := range vars.Custom {
		placeholder := fmt.Sprintf("{{.%s}}", k)
		result = strings.ReplaceAll(result, placeholder, v)
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
