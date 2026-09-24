package tasks

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	testForwardHTML  = `<div class="gmail_quote">---------- Forwarded message ---------<br>From: a@example.com<br></div><p>Original</p>`
	testForwardPlain = "---------- Forwarded message ---------\nFrom: a@example.com\n\nOriginal"
)

func signedMailbox() *models.Email {
	return &models.Email{SignatureSync: true, SignatureHTML: "<b>Ana</b>", SignaturePlain: "-- Ana"}
}

// Note, signature, forwarded message: the order every mail client writes a forward in.
func TestUserEmailBodiesForwardOrder(t *testing.T) {
	task := &repository.EmailTask{
		BodyHTML:       "<div>FYI</div>",
		BodyPlain:      "FYI",
		ForwardedHTML:  testForwardHTML,
		ForwardedPlain: testForwardPlain,
	}
	html, plain := userEmailBodies(task, signedMailbox())

	if want := "FYI\n\n-- Ana\n\n" + testForwardPlain; plain != want {
		t.Fatalf("plain:\n got %q\nwant %q", plain, want)
	}
	note, sig, fwd := strings.Index(html, "FYI"), strings.Index(html, "<b>Ana</b>"), strings.Index(html, "Forwarded message")
	if note < 0 || sig < 0 || fwd < 0 || !(note < sig && sig < fwd) {
		t.Fatalf("html out of order (note %d, signature %d, forward %d):\n%s", note, sig, fwd, html)
	}
}

// A forward with no note still carries the message, signed above it.
func TestUserEmailBodiesForwardWithoutNote(t *testing.T) {
	task := &repository.EmailTask{ForwardedHTML: testForwardHTML, ForwardedPlain: testForwardPlain}

	html, plain := userEmailBodies(task, signedMailbox())
	if want := "-- Ana\n\n" + testForwardPlain; plain != want {
		t.Fatalf("plain:\n got %q\nwant %q", plain, want)
	}
	if sig, fwd := strings.Index(html, "<b>Ana</b>"), strings.Index(html, "Forwarded message"); sig < 0 || fwd < sig {
		t.Fatalf("html should be signature then forward:\n%s", html)
	}

	html, plain = userEmailBodies(task, &models.Email{})
	if plain != testForwardPlain || !strings.Contains(html, "<p>Original</p>") {
		t.Fatalf("unsigned forward: html %q plain %q", html, plain)
	}
}

// A reply is unchanged: no forward, and no signature on an empty part.
func TestUserEmailBodiesReplyUnchanged(t *testing.T) {
	html, plain := userEmailBodies(&repository.EmailTask{BodyPlain: "Thanks"}, signedMailbox())
	if html != "" || plain != "Thanks\n\n-- Ana" {
		t.Fatalf("reply: html %q plain %q", html, plain)
	}
}

// Leading blank lines an author typed are theirs; only an empty note is trimmed.
func TestUserEmailBodiesKeepsLeadingBlankLines(t *testing.T) {
	_, plain := userEmailBodies(&repository.EmailTask{BodyPlain: "\n\nSpaced"}, signedMailbox())
	if plain != "\n\nSpaced\n\n-- Ana" {
		t.Fatalf("reply spacing changed: %q", plain)
	}
}
