package smtp

import (
	"bytes"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"slices"
	"strings"
	"testing"
)

// A body with no HTML goes out as one text/plain part. Wrapping a lone part in
// multipart/alternative is scored by content filters on small mail hosts.
func TestPlainOnlySendIsASingleTextPart(t *testing.T) {
	srv := newFakeServer(t, "")
	host, port := srv.addr()
	c := newTestClient(host, port)

	raw, err := c.Send(t.Context(), "Ana", []string{"to@example.test"}, nil, nil,
		"id@warmbly.test", "Quick note", "Hi,\n\nSee you Tuesday.", "", "", nil,
		map[string]string{"X-Mailtrace-Verify": "token"})
	if err != nil {
		t.Fatalf("send: %v", err.Message)
	}
	msg, perr := mail.ReadMessage(bytes.NewReader(raw))
	if perr != nil {
		t.Fatalf("parse sent message: %v", perr)
	}
	mediaType, _, merr := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if merr != nil || mediaType != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", msg.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if got := string(body); got != "Hi,\r\n\r\nSee you Tuesday." {
		t.Errorf("body = %q", got)
	}
	if msg.Header.Get("X-Mailtrace-Verify") != "token" {
		t.Error("custom header lost")
	}
}

// Both bodies still make a multipart/alternative.
func TestPlainAndHTMLSendIsAlternative(t *testing.T) {
	srv := newFakeServer(t, "")
	host, port := srv.addr()
	c := newTestClient(host, port)

	raw, err := c.Send(t.Context(), "", []string{"to@example.test"}, nil, nil,
		"id@warmbly.test", "Hi", "plain", "<p>html</p>", "", nil)
	if err != nil {
		t.Fatalf("send: %v", err.Message)
	}
	msg, _ := mail.ReadMessage(bytes.NewReader(raw))
	if mediaType, _, _ := mime.ParseMediaType(msg.Header.Get("Content-Type")); mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q, want multipart/alternative", msg.Header.Get("Content-Type"))
	}
}

// Headers go out in one fixed order, so no two sends differ in a way a filter
// can fingerprint.
func TestHeaderOrderIsStable(t *testing.T) {
	names := func(raw []byte) []string {
		head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
		var out []string
		for _, line := range strings.Split(head, "\r\n") {
			if name, _, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, " ") {
				out = append(out, name)
			}
		}
		return out
	}

	srv := newFakeServer(t, "")
	host, port := srv.addr()
	c := newTestClient(host, port)
	var first []string
	for i := range 8 {
		raw, err := c.Send(t.Context(), "Ana", []string{"to@example.test"}, nil, nil,
			"id@warmbly.test", "Re: hi", "body", "", "parent@example.test", nil,
			map[string]string{"X-Mailtrace-Verify": "token"})
		if err != nil {
			t.Fatalf("send: %v", err.Message)
		}
		got := names(raw)
		if i == 0 {
			first = got
			continue
		}
		if !slices.Equal(got, first) {
			t.Fatalf("header order changed between sends:\n%v\n%v", first, got)
		}
	}
	want := []string{"Date", "From", "To", "Message-ID", "In-Reply-To", "References", "Subject",
		"MIME-Version", "Content-Type", "Content-Transfer-Encoding", "X-Mailtrace-Verify"}
	if !slices.Equal(first, want) {
		t.Errorf("header order = %v, want %v", first, want)
	}
}
