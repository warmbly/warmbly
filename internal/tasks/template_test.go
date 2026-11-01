package tasks

import (
	htmltemplate "html/template"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestCompiledTemplateCachesRawSource(t *testing.T) {
	for _, source := range []string{
		`{{.unique-job title}}`,
		`{{if .unique-job title}}broken {{.FirstName}}`,
	} {
		first := compiledTemplate(source)
		cached, ok := tmplCache.Load(source)
		if !ok || cached != first || compiledTemplate(source) != first {
			t.Fatalf("raw source was not cached: %q", source)
		}
		for _, value := range []string{"Founder", "Engineer"} {
			contact := models.Contact{FirstName: "Alex", CustomFields: map[string]string{"unique-job title": value}}
			want := value
			if first == nil {
				want = "{{if .unique-job title}}broken Alex"
			}
			if got := RenderTemplate(source, contact); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		}
	}
}

func TestRenderTemplateCachedConcurrent(t *testing.T) {
	source := `Hello {{.FirstName}} in {{.job title}} from {{.Sender.Name}}`
	for _, name := range []string{"Alex", "Ana", "Sam", "Jo"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			contact := models.Contact{FirstName: name, CustomFields: map[string]string{"job title": name}}
			context := TemplateContext{Sender: TemplateSender{Name: name}}
			for i := 0; i < 100; i++ {
				if got, want := RenderTemplateWith(source, contact, context), "Hello "+name+" in "+name+" from "+name; got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestRenderTemplate_Sender(t *testing.T) {
	stamp := time.Date(2026, 10, 5, 15, 0, 0, 0, time.FixedZone("local", 3600))
	account := &models.Email{
		Name: " Tareque M. ", Email: "tareque@example.com", SendAsEmail: "hello@example.com",
		ReplyTo: "replies@example.com", Provider: "gmail", Timezone: "Europe/Budapest",
		SignaturePlain: "Best, Tareque", SignatureHTML: "<b>Tareque</b>", SignatureSync: true,
		CampaignLimit: 25, Tags: []string{"sales", "team"}, CreatedAt: stamp, Warmup: &stamp,
	}
	contact := models.Contact{FirstName: "Alex", Email: "alex@example.org", CustomFields: map[string]string{
		"role": "CTO", "Job Title": "Founder", "Sender": "not the sender", "FirstName": "not Alex",
	}}
	context := templateContext(account, "https://example.com/unsubscribe/tok")
	cases := []struct{ name, template, want string }{
		{"identity", `Hi {{.FirstName}}, I'm {{.Sender.Name}} <{{.Sender.Email}}>`, "Hi Alex, I'm Tareque M. <hello@example.com>"},
		{"addresses", `{{.Email}}|{{.Sender.MailboxEmail}}|{{.Sender.SendAsEmail}}|{{.Sender.ReplyTo}}`, "alex@example.org|tareque@example.com|hello@example.com|replies@example.com"},
		{"contact and link", `{{.role}}|{{.Job Title}}|{{.UnsubscribeLink}}`, "CTO|Founder|https://example.com/unsubscribe/tok"},
		{"native types", `{{if .Sender.SignatureSync}}{{.Sender.SignaturePlain}}{{end}}|{{if gt .Sender.CampaignLimit 20}}high{{end}}`, "Best, Tareque|high"},
		{"with and range", `{{with .Sender}}{{.Name}}:{{range .Tags}}{{.}};{{end}}{{end}}`, "Tareque M.:sales;team;"},
		{"helpers", `{{.Sender.Name | upper}}|{{.Sender.Vendor | default "direct"}}`, "TAREQUE M.|direct"},
		{"timestamps", `{{.Sender.CreatedAt}}|{{.Sender.Warmup}}|{{.Sender.WarmupPausedAt}}`, "2026-10-05T14:00:00Z|2026-10-05T14:00:00Z|"},
		{"html", `{{.Sender.SignatureHTML}}`, "<b>Tareque</b>"},
		{"malformed fallback", `{{if .Company}}Hi {{.FirstName}}, {{.Sender.Name}} at {{.Sender.Email}}`, "{{if .Company}}Hi Alex, Tareque M. at hello@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderTemplateWith(tc.template, contact, context); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	account.Tags[0] = "changed"
	if context.Sender.Tags[0] != "sales" {
		t.Fatal("sender tags share the mailbox's mutable slice")
	}
}

func TestTemplateSender_EmptyAndEffectiveIdentity(t *testing.T) {
	if got := RenderTemplate(`{{.Sender.Name}}|{{.Sender.Email}}|{{.Sender.Name | default "our team"}}|{{if .Sender.SignatureSync}}yes{{else}}no{{end}}`, models.Contact{}); got != "||our team|no" {
		t.Fatalf("empty sender: %q", got)
	}
	account := &models.Email{Name: "John S.", Email: "john@example.com", OrgTimezone: "America/New_York"}
	sender := templateContext(account, "").Sender
	if sender.Email != "john@example.com" || sender.MailboxEmail != sender.Email || sender.SendAsEmail != "" || sender.ReplyTo != "" || sender.Timezone != "America/New_York" {
		t.Fatalf("incorrect effective sender: %+v", sender)
	}
	account.ReplyTo = "john@example.com"
	if templateContext(account, "").Sender.ReplyTo != "" {
		t.Fatal("redundant Reply-To did not match the outbound header")
	}
}

func TestTemplateSender_SplitsName(t *testing.T) {
	cases := []struct{ name, want string }{
		{"  Jamie   van der  Berg ", "Jamie|van der Berg|Jamie   van der  Berg"},
		{"Jamie", "Jamie||Jamie"},
		{"   ", "||"},
	}
	for _, tc := range cases {
		context := templateContext(&models.Email{Name: tc.name}, "")
		if got := RenderTemplateWith(`{{.Sender.FirstName}}|{{.Sender.LastName}}|{{.Sender.Name}}`, models.Contact{}, context); got != tc.want {
			t.Errorf("name %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAIVarAvailableVarsSkipsReservedSenderNamespace(t *testing.T) {
	vars := aiVarAvailableVars(&models.Contact{CustomFields: map[string]string{"Sender": "not a mailbox", "UnsubscribeLink": "not a link", "role": "CTO"}})
	for _, token := range vars {
		if token == "{{.Sender}}" || token == "{{.UnsubscribeLink}}" {
			t.Fatalf("AI advertised a reserved contact field: %s", token)
		}
	}
	if !strings.Contains(strings.Join(vars, " "), "{{.role}}") {
		t.Fatal("valid custom field was excluded")
	}
}

func TestTemplateSender_ExcludesInternalFieldsAndMethods(t *testing.T) {
	typ := reflect.TypeOf(TemplateSender{})
	for _, field := range []string{"ID", "UserID", "OrganizationID", "WorkerID", "DomainGrantID", "VendorConnectionID", "LastID", "OrgTimezone", "Password", "AccessToken", "RefreshToken"} {
		if _, ok := typ.FieldByName(field); ok {
			t.Errorf("unsafe sender field %s", field)
		}
	}
	if typ.NumMethod() != 0 || reflect.PointerTo(typ).NumMethod() != 0 {
		t.Fatal("sender exposes mailbox methods")
	}
	for _, field := range []string{"UserID", "WorkerID", "AccessToken", "SendFrom"} {
		tmpl := "{{.Sender." + field + "}}"
		preview := previewTemplatesWith(tmpl, "", "", models.Contact{}, templateContext(&models.Email{UserID: "private"}, ""))
		if preview.Subject != tmpl || len(preview.Unresolved) != 1 {
			t.Errorf("unknown sender field %s should remain unresolved: %+v", field, preview)
		}
	}
}

func TestTemplateSender_MapsEveryAllowedMailboxField(t *testing.T) {
	account := &models.Email{}
	mailbox := reflect.ValueOf(account).Elem()
	stamp := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	for i := 0; i < mailbox.NumField(); i++ {
		field := mailbox.Field(i)
		switch field.Kind() {
		case reflect.String:
			field.SetString(mailbox.Type().Field(i).Name + "-value")
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Int:
			field.SetInt(37)
		default:
			switch field.Type() {
			case reflect.TypeOf(stamp):
				field.Set(reflect.ValueOf(stamp))
			case reflect.TypeOf(&stamp):
				field.Set(reflect.ValueOf(&stamp))
			case reflect.TypeOf([]string{}):
				field.Set(reflect.ValueOf([]string{"sales"}))
			}
		}
	}
	sender := reflect.ValueOf(templateContext(account, "").Sender)
	for i := 0; i < sender.NumField(); i++ {
		name := sender.Type().Field(i).Name
		if name == "FirstName" || name == "LastName" {
			continue // derived from Name, covered by TestTemplateSender_SplitsName
		}
		mailboxName := name
		if name == "MailboxEmail" {
			mailboxName = "Email"
		}
		var want any = mailbox.FieldByName(mailboxName).Interface()
		switch v := want.(type) {
		case time.Time:
			want = templateTime(&v)
		case *time.Time:
			want = templateTime(v)
		}
		if name == "Email" {
			want = account.SendFrom()
		}
		if name == "ReplyTo" {
			want = account.ReplyToHeader()
		}
		if name == "SignatureHTML" {
			want = htmltemplate.HTML(account.SignatureHTML)
		}
		if !reflect.DeepEqual(sender.Field(i).Interface(), want) {
			t.Errorf("Sender.%s = %v, want %v", name, sender.Field(i).Interface(), want)
		}
	}
}

func TestRenderTemplate_RangeOverNumberRefused(t *testing.T) {
	tmpl := "Hi {{.FirstName}}{{range 100000000000}}x{{end}}"
	if err := TemplateError(tmpl); err == nil {
		t.Fatal("TemplateError accepted a range over a number")
	}
	done := make(chan string, 1)
	go func() { done <- RenderTemplate(tmpl, models.Contact{FirstName: "Ann"}) }()
	select {
	case out := <-done:
		if strings.Contains(out, "xx") {
			t.Fatalf("range executed: %d bytes", len(out))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render of a range over a number did not return")
	}
}

func TestRenderTemplate_RangeOverMapStillRenders(t *testing.T) {
	contact := models.Contact{FirstName: "Ann"}
	out := RenderTemplate(`{{range $k, $v := .}}{{if eq $k "FirstName"}}{{$v}}{{end}}{{end}}`, contact)
	if out != "Ann" {
		t.Fatalf("got %q", out)
	}
}

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

func TestGenerateMessageID_Format(t *testing.T) {
	mid := generateMessageID("user@example.com")
	if !strings.HasSuffix(mid, "@example.com>") {
		t.Errorf("message ID should end with domain, got %q", mid)
	}
	if !strings.HasPrefix(mid, "<") {
		t.Errorf("message ID should start with <, got %q", mid)
	}
}

// The HTML signature is a block with a top margin, not <br><br> stacked on the
// body's own trailing margin, and it goes inside the document when there is one.
func TestAddSignatureSpacing(t *testing.T) {
	out := AddSignature("<p>Hi</p>", "<p>Ana</p>", true)
	if strings.Contains(out, "<br>") {
		t.Errorf("signature still separated by breaks: %q", out)
	}
	if !strings.Contains(out, `<div style="margin-top:16px"><p>Ana</p></div>`) {
		t.Errorf("signature not wrapped in its own block: %q", out)
	}

	doc := AddSignature("<html><body><p>Hi</p></body></html>", "<p>Ana</p>", true)
	if !strings.HasSuffix(doc, "</body></html>") || strings.Index(doc, "Ana") > strings.Index(doc, "</body>") {
		t.Errorf("signature landed outside the document: %q", doc)
	}

	// HTML tag names are case-insensitive, so a pasted </BODY> counts too.
	upper := AddSignature("<HTML><BODY><p>Hi</p></BODY></HTML>", "<p>Ana</p>", true)
	if !strings.HasSuffix(upper, "</BODY></HTML>") || strings.Index(upper, "Ana") > strings.Index(upper, "</BODY>") {
		t.Errorf("signature landed outside an upper-case document: %q", upper)
	}
	spaced := AddSignature("<html><body><p>Hi</p></body ></html>", "<p>Ana</p>", true)
	if strings.Index(spaced, "Ana") > strings.Index(spaced, "</body >") {
		t.Errorf("signature landed outside a spaced closing tag: %q", spaced)
	}

	if plain := AddSignature("Hi", "Ana", false); plain != "Hi\n\nAna" {
		t.Errorf("plain-text spacing changed: %q", plain)
	}
	if none := AddSignature("<p>Hi</p>", "", true); none != "<p>Hi</p>" {
		t.Errorf("empty signature altered the body: %q", none)
	}
}

// The opt-out footer still lands after the signature once both are applied to a
// body carrying a </body>, which is the order a reader expects.
func TestSignatureThenOptOutOrderInsideDocument(t *testing.T) {
	body := AddSignature("<html><body><p>Hi</p></body></html>", "<p>Ana</p>", true)
	body, _ = appendOptOut(body, "", models.UnsubscribeSettings{Mode: models.UnsubscribeModeText, Text: "Reply stop."}, "")
	sig, foot := strings.Index(body, "Ana"), strings.Index(body, "Reply stop.")
	if sig < 0 || foot < 0 || sig > foot {
		t.Fatalf("footer did not follow the signature: %q", body)
	}
	if strings.Index(body, "</body>") < foot {
		t.Fatalf("footer landed outside the document: %q", body)
	}
}
