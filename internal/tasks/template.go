package tasks

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
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

// GenerateConversationEmail generates email content from AI conversation
func GenerateConversationEmail(conversation Conversation, account models.Email, isReply bool) string {
	// TODO: Implement AI conversation generation
	// For now, return a simple placeholder

	if isReply {
		return fmt.Sprintf("Thanks for your email! I appreciate you reaching out.\n\nBest regards,\n%s", account.Name)
	}

	return fmt.Sprintf("Hi,\n\n%s\n\nBest regards,\n%s",
		conversation.Description,
		account.Name)
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
