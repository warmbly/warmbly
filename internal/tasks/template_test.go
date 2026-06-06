package tasks

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestRenderTemplate_BasicVariables(t *testing.T) {
	contact := models.Contact{
		FirstName: "Alice",
		LastName:  "Smith",
		Email:     "alice@example.com",
		Company:   "Acme Corp",
		Phone:     "+1234567890",
	}

	tmpl := "Hi {{.FirstName}} {{.LastName}}, welcome from {{.Company}}!"
	result := RenderTemplate(tmpl, contact)

	expected := "Hi Alice Smith, welcome from Acme Corp!"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestRenderTemplate_CustomFields(t *testing.T) {
	contact := models.Contact{
		FirstName:    "Bob",
		CustomFields: map[string]string{"role": "Engineer", "city": "Berlin"},
	}

	tmpl := "Hey {{.FirstName}}, you work as a {{.role}} in {{.city}}"
	result := RenderTemplate(tmpl, contact)

	if !strings.Contains(result, "Engineer") || !strings.Contains(result, "Berlin") {
		t.Errorf("custom fields not rendered: %q", result)
	}
}

func TestRenderTemplate_EmptyContact(t *testing.T) {
	contact := models.Contact{}
	tmpl := "Hello {{.FirstName}}"
	result := RenderTemplate(tmpl, contact)

	if result != "Hello " {
		t.Errorf("expected empty first name, got %q", result)
	}
}

func TestRenderTemplate_NoPlaceholders(t *testing.T) {
	contact := models.Contact{FirstName: "Test"}
	tmpl := "Just a plain text email with no variables."
	result := RenderTemplate(tmpl, contact)

	if result != tmpl {
		t.Errorf("expected unchanged text, got %q", result)
	}
}

func TestRenderTemplate_ConditionalIfSet(t *testing.T) {
	tmpl := "Hi {{.FirstName}},{{if .Company}} saw {{.Company}} is hiring.{{end}}"

	with := RenderTemplate(tmpl, models.Contact{FirstName: "Alex", Company: "Acme"})
	if with != "Hi Alex, saw Acme is hiring." {
		t.Errorf("if-set (present) wrong: %q", with)
	}

	without := RenderTemplate(tmpl, models.Contact{FirstName: "Alex"})
	if without != "Hi Alex," {
		t.Errorf("if-set (absent) wrong: %q", without)
	}
}

func TestRenderTemplate_IfElse(t *testing.T) {
	tmpl := "{{if .FirstName}}Hi {{.FirstName}}{{else}}Hi there{{end}},"

	if got := RenderTemplate(tmpl, models.Contact{FirstName: "Sam"}); got != "Hi Sam," {
		t.Errorf("if branch wrong: %q", got)
	}
	if got := RenderTemplate(tmpl, models.Contact{}); got != "Hi there," {
		t.Errorf("else branch wrong: %q", got)
	}
}

func TestRenderTemplate_EqOnCustomField(t *testing.T) {
	tmpl := `{{if eq .city "Berlin"}}in town{{else}}remote{{end}}`

	yes := RenderTemplate(tmpl, models.Contact{CustomFields: map[string]string{"city": "Berlin"}})
	if yes != "in town" {
		t.Errorf("eq match wrong: %q", yes)
	}
	no := RenderTemplate(tmpl, models.Contact{CustomFields: map[string]string{"city": "Paris"}})
	if no != "remote" {
		t.Errorf("eq non-match wrong: %q", no)
	}
}

func TestRenderTemplate_MissingKeyRendersEmpty(t *testing.T) {
	// An unknown token renders empty (missingkey=zero) rather than leaking.
	got := RenderTemplate("X{{.Nope}}Y", models.Contact{FirstName: "A"})
	if got != "XY" {
		t.Errorf("missing key should be empty: %q", got)
	}
}

func TestRenderTemplate_MalformedFallsBack(t *testing.T) {
	// An {{if}} with no {{end}} must not hard-fail; it falls back to naive
	// substitution so standard variables still resolve.
	got := RenderTemplate("Hi {{.FirstName}} {{if .Company}}oops", models.Contact{FirstName: "Bo", Company: "Acme"})
	if !strings.Contains(got, "Bo") {
		t.Errorf("malformed template should still substitute variables: %q", got)
	}
}

func TestRenderTemplate_NonIdentifierCustomKey(t *testing.T) {
	// Custom keys with spaces can't use selector syntax; the pre-pass substitutes
	// {{.Job Title}} literally so it still renders.
	got := RenderTemplate("Role: {{.Job Title}}", models.Contact{
		CustomFields: map[string]string{"Job Title": "CTO"},
	})
	if got != "Role: CTO" {
		t.Errorf("non-identifier custom key wrong: %q", got)
	}
}

func TestGenerateConversationEmail_NewEmail(t *testing.T) {
	conv := Conversation{
		Theme:       "test",
		Description: "This is a test conversation.",
		Messages:    []string{"What do you think?"},
	}
	account := models.Email{Name: "John Doe", Email: "john@test.com"}

	body := GenerateConversationEmail(conv, account, false)

	if !strings.Contains(body, "This is a test conversation.") {
		t.Errorf("body should contain description: %q", body)
	}
	if !strings.Contains(body, "John Doe") {
		t.Errorf("body should contain signature: %q", body)
	}
}

func TestGenerateConversationEmail_Reply(t *testing.T) {
	conv := Conversation{
		Theme:       "test",
		Description: "Test desc.",
		Messages:    []string{"Sure thing!"},
	}
	account := models.Email{Name: "Jane", Email: "jane@test.com"}

	body := GenerateConversationEmail(conv, account, true)

	if strings.Contains(body, "Test desc.") {
		t.Errorf("reply should not contain description: %q", body)
	}
	if !strings.Contains(body, "Jane") {
		t.Errorf("reply should contain signature: %q", body)
	}
}

func TestGenerateConversationEmail_FallbackSignature(t *testing.T) {
	conv := Conversation{Description: "Hello.", Messages: []string{"Hi"}}
	account := models.Email{Email: "anon@test.com"} // No Name set

	body := GenerateConversationEmail(conv, account, false)

	if !strings.Contains(body, "anon@test.com") {
		t.Errorf("should fall back to email as signature: %q", body)
	}
}

func TestExtractPlainTextFromHTML(t *testing.T) {
	html := "<p>Hello <b>world</b></p><br><p>Second paragraph</p>"
	plain := ExtractPlainTextFromHTML(html)

	if !strings.Contains(plain, "Hello") || !strings.Contains(plain, "world") {
		t.Errorf("plain text should contain content: %q", plain)
	}
	if strings.Contains(plain, "<p>") || strings.Contains(plain, "<b>") {
		t.Errorf("plain text should not contain HTML tags: %q", plain)
	}
}

func TestGenerateWarmupSubject_NotEmpty(t *testing.T) {
	subject := generateWarmupSubject()
	if subject == "" {
		t.Error("warmup subject should not be empty")
	}
}

func TestRandomWarmupConversation_HasContent(t *testing.T) {
	conv := randomWarmupConversation()
	if conv.Theme == "" {
		t.Error("conversation should have a theme")
	}
	if conv.Description == "" {
		t.Error("conversation should have a description")
	}
	if len(conv.Messages) == 0 {
		t.Error("conversation should have at least one message")
	}
}

func TestConversationForTheme_MatchesKnownTheme(t *testing.T) {
	conv := conversationForTheme("productivity")
	if conv.Theme != "productivity" {
		t.Errorf("expected theme productivity, got %q", conv.Theme)
	}
}

func TestConversationForTheme_FallsBackOnUnknown(t *testing.T) {
	conv := conversationForTheme("not-a-real-theme")
	if conv.Theme == "" {
		t.Error("fallback conversation should still have a theme")
	}
}

func TestConversationForTheme_EmptyReturnsRandom(t *testing.T) {
	conv := conversationForTheme("")
	if conv.Theme == "" {
		t.Error("empty theme should return a random conversation with a theme")
	}
}

func TestGenerateMessageID_Format(t *testing.T) {
	mid := generateMessageID("user@example.com")
	if !strings.HasSuffix(mid, "@example.com>") {
		t.Errorf("message ID should end with domain, got %q", mid)
	}
	if !strings.HasPrefix(mid, "<") {
		t.Errorf("message ID should start with <, got %q", mid)
	}
}
