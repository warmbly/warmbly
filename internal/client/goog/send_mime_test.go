package goog

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func baseHeaders() []header {
	return []header{
		{"From", "Jane Doe <jane@example.com>"},
		{"To", "lead@example.com"},
		{"Subject", "Hello"},
		{"Message-ID", "<abc@example.com>"},
		{"MIME-Version", "1.0"},
	}
}

// parseMIME reads the built message back the way a receiving MTA would, so the
// tests assert on a parsed message rather than on string fragments.
func parseMIME(t *testing.T, raw []byte) (*mail.Message, string, map[string]string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("the built message does not parse as RFC 5322: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Content-Type does not parse: %v", err)
	}
	return msg, mediaType, params
}

// A message with no HTML and no attachments should be a bare text/plain, not a
// multipart wrapper around a single part.
func TestBuildMIMEPlainOnly(t *testing.T) {
	raw, err := buildMIME(baseHeaders(), "just text", "", nil)
	if err != nil {
		t.Fatalf("buildMIME: %v", err)
	}

	msg, mediaType, _ := parseMIME(t, raw)
	if mediaType != "text/plain" {
		t.Errorf("got Content-Type %q, want text/plain", mediaType)
	}
	if got := msg.Header.Get("Message-ID"); got != "<abc@example.com>" {
		t.Errorf("Message-ID = %q", got)
	}
}

func TestBuildMIMEAlternativeWhenHTMLPresent(t *testing.T) {
	raw, err := buildMIME(baseHeaders(), "just text", "<p>rich</p>", nil)
	if err != nil {
		t.Fatalf("buildMIME: %v", err)
	}

	msg, mediaType, params := parseMIME(t, raw)
	if mediaType != "multipart/alternative" {
		t.Fatalf("got Content-Type %q, want multipart/alternative", mediaType)
	}

	types := partTypes(t, msg.Body, params["boundary"])
	want := []string{"text/plain", "text/html"}
	if len(types) != len(want) {
		t.Fatalf("got %d parts (%v), want %v", len(types), types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("part %d is %q, want %q", i, types[i], want[i])
		}
	}
}

// Attachments are the only case that needs the multipart/mixed wrapper.
func TestBuildMIMEMixedWhenAttachmentsPresent(t *testing.T) {
	att := []Attachment{{Filename: "a.txt", MimeType: "text/plain", Data: []byte("hi")}}
	raw, err := buildMIME(baseHeaders(), "text", "<p>rich</p>", att)
	if err != nil {
		t.Fatalf("buildMIME: %v", err)
	}

	msg, mediaType, params := parseMIME(t, raw)
	if mediaType != "multipart/mixed" {
		t.Fatalf("got Content-Type %q, want multipart/mixed", mediaType)
	}

	types := partTypes(t, msg.Body, params["boundary"])
	want := []string{"multipart/alternative", "text/plain"}
	if len(types) != len(want) {
		t.Fatalf("got %d parts (%v), want %v", len(types), types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("part %d is %q, want %q", i, types[i], want[i])
		}
	}
}

// Every send now builds its own headers, so a non-ASCII subject has to be
// RFC 2047-encoded rather than emitted as raw 8-bit bytes.
func TestSubjectEncoding(t *testing.T) {
	encoded := mime.QEncoding.Encode("utf-8", "Café renovation")
	if !strings.HasPrefix(encoded, "=?utf-8?") {
		t.Fatalf("non-ASCII subject was not encoded: %q", encoded)
	}

	hdrs := append(baseHeaders()[:2], header{"Subject", encoded}, header{"MIME-Version", "1.0"})
	raw, err := buildMIME(hdrs, "body", "", nil)
	if err != nil {
		t.Fatalf("buildMIME: %v", err)
	}

	msg, _, _ := parseMIME(t, raw)
	got, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	if got != "Café renovation" {
		t.Errorf("subject round-tripped as %q", got)
	}

	// An ASCII subject must not be needlessly encoded.
	if plain := mime.QEncoding.Encode("utf-8", "Hello"); plain != "Hello" {
		t.Errorf("ASCII subject was encoded to %q", plain)
	}
}

// A display name with a comma splits the From header into two addresses unless
// it is quoted, and a non-ASCII one has to be encoded.
func TestGetAddressQuotesAndEncodes(t *testing.T) {
	c := &Client{FirstName: "Doe,", LastName: "Jane", Email: "jane@example.com"}
	addrs, err := mail.ParseAddressList(c.GetAddress())
	if err != nil {
		t.Fatalf("From does not parse: %v", err)
	}
	if len(addrs) != 1 {
		t.Fatalf("From parsed as %d addresses, want 1", len(addrs))
	}
	if addrs[0].Address != "jane@example.com" {
		t.Errorf("address = %q", addrs[0].Address)
	}

	accented := &Client{FirstName: "Renée", LastName: "", Email: "renee@example.com"}
	parsed, err := mail.ParseAddress(accented.GetAddress())
	if err != nil {
		t.Fatalf("accented From does not parse: %v", err)
	}
	if parsed.Name != "Renée" {
		t.Errorf("display name round-tripped as %q", parsed.Name)
	}
}

func partTypes(t *testing.T, body io.Reader, boundary string) []string {
	t.Helper()
	if boundary == "" {
		t.Fatal("multipart Content-Type carried no boundary")
	}

	var types []string
	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading parts: %v", err)
		}
		mt, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("part Content-Type does not parse: %v", err)
		}
		types = append(types, mt)
	}
	return types
}
