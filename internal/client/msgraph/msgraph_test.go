package msgraph

import (
	"net/url"

	"golang.org/x/oauth2/clientcredentials"
	"slices"
	"strings"
	"testing"
)

func TestBuildMIME_HeadersThreadingAndBodies(t *testing.T) {
	hdrs := []hdr{
		{"From", "a@b.com"},
		{"To", "c@d.com"},
		{"Subject", "hi"},
		{"Message-ID", "<mid@x>"},
		{"In-Reply-To", "<parent@x>"},
		{"References", "<parent@x>"},
		{"X-Mailtrace-Verify", "tok"},
	}
	raw, err := buildMIME(hdrs, "plain body", "<p>html</p>", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"Message-ID: <mid@x>",
		"In-Reply-To: <parent@x>",
		"References: <parent@x>",
		"X-Mailtrace-Verify: tok",
		"multipart/alternative",
		"text/html",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("MIME missing %q", want)
		}
	}
}

func TestBuildMIME_PlainOnly(t *testing.T) {
	raw, err := buildMIME([]hdr{{"From", "a@b.com"}, {"Subject", "s"}}, "just text", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "multipart") {
		t.Error("plain-only message should not be multipart")
	}
}

func TestBuildMIME_Attachment(t *testing.T) {
	raw, err := buildMIME(
		[]hdr{{"From", "a@b.com"}},
		"body", "",
		[]Attachment{{Filename: "f.txt", MimeType: "text/plain", Data: []byte("data")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "multipart/mixed") {
		t.Error("attachment message must be multipart/mixed")
	}
	if !strings.Contains(s, `filename="f.txt"`) {
		t.Error("attachment filename missing")
	}
}

func TestToEmailData_MappingAndFlags(t *testing.T) {
	m := &graphMessage{
		ID:                "gid",
		InternetMessageID: "<mid@x>",
		ConversationID:    "conv",
		Subject:           "hello",
		IsRead:            true,
		From:              &graphRecipient{EmailAddress: graphEmailAddress{Name: "Alice", Address: "a@b.com"}},
		ToRecipients:      []graphRecipient{{EmailAddress: graphEmailAddress{Address: "c@d.com"}}},
		Body:              &graphItemBody{ContentType: "text", Content: "hello body"},
		InternetMessageHeaders: []graphHeader{
			{Name: "X-Mailtrace-Verify", Value: "tok"},
			{Name: "Auto-Submitted", Value: "auto-replied"},
		},
	}
	d := m.toEmailData()
	if d.GmailID != "gid" || d.MessageID != "<mid@x>" || d.ThreadID != "conv" {
		t.Errorf("ids wrong: %+v", d)
	}
	if !slices.Contains(d.Flags, "\\Seen") {
		t.Error("isRead should map to \\Seen")
	}
	if !slices.Contains(d.Flags, "X-Mailtrace-Verify:tok") {
		t.Errorf("warmup pseudo-flag missing: %v", d.Flags)
	}
	if d.BodyPlain != "hello body" {
		t.Errorf("text body wrong: %q", d.BodyPlain)
	}
	if len(d.From) != 1 || d.From[0] != "Alice <a@b.com>" {
		t.Errorf("from formatting: %v", d.From)
	}
}

func TestClientUsesMePathsForDelegatedMailboxes(t *testing.T) {
	c := &Client{}
	if got, want := c.mailboxBaseURL(), graphBase+"/me"; got != want {
		t.Fatalf("delegated mailbox base = %q, want %q", got, want)
	}
	if got, want := c.messageURL("abc/def"), graphBase+"/me/messages/abc%2Fdef"; got != want {
		t.Fatalf("delegated message url = %q, want %q", got, want)
	}
}

func TestClientUsesUsersPathsForAppOnlyMailboxTarget(t *testing.T) {
	c := &Client{MailboxUserID: "James.Smith+shared@example.com"}
	if got, want := c.mailboxBaseURL(), graphBase+"/users/"+url.PathEscape("James.Smith+shared@example.com"); got != want {
		t.Fatalf("app-only mailbox base = %q, want %q", got, want)
	}
	if got, want := c.messageURL("abc/def"), graphBase+"/users/"+url.PathEscape("James.Smith+shared@example.com")+"/messages/abc%2Fdef"; got != want {
		t.Fatalf("app-only message url = %q, want %q", got, want)
	}
}

func TestInitAppOnlyRequiresMailboxTarget(t *testing.T) {
	c := &Client{}
	err := c.InitAppOnly(t.Context(), clientcredentials.Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		Scopes:       []string{"https://graph.microsoft.com/.default"},
	}, "")
	if err == nil {
		t.Fatal("expected missing mailbox target to fail")
	}
}

func TestMailboxBaseTargetsSharedMailboxForDelegatedSharedPayloads(t *testing.T) {
	c := &Client{Email: "delegate@example.com", MailboxEmail: "Shared Mailbox@example.com"}
	got := c.mailboxBase()
	want := graphBase + "/users/Shared%20Mailbox@example.com"
	if got != want {
		t.Fatalf("mailboxBase() = %q, want %q", got, want)
	}
}

func TestMailboxBaseFallsBackToEmailForLegacyPayloads(t *testing.T) {
	c := &Client{Email: "sender@example.com"}
	got := c.mailboxBase()
	want := graphBase + "/users/sender@example.com"
	if got != want {
		t.Fatalf("mailboxBase() = %q, want %q", got, want)
	}
}
