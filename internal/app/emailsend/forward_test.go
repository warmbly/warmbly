package emailsend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func forwardedFixture() *models.ForwardedMessage {
	return &models.ForwardedMessage{
		From:      []string{"John Smith <john@example.com>"},
		To:        []string{"sales@example.com"},
		CC:        []string{"finance@example.com"},
		Subject:   "Pricing details",
		Date:      time.Date(2026, 9, 23, 8, 34, 0, 0, time.UTC),
		BodyPlain: "Hi,\n\nHere are the pricing details...",
	}
}

func TestRenderForwardedPlain(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	_, plain := renderForwarded(forwardedFixture(), loc)

	want := strings.Join([]string{
		"---------- Forwarded message ---------",
		"From: John Smith <john@example.com>",
		"Date: Wed, Sep 23, 2026 at 10:34 AM CEST",
		"Subject: Pricing details",
		"To: sales@example.com",
		"Cc: finance@example.com",
		"",
		"Hi,",
		"",
		"Here are the pricing details...",
	}, "\n")
	if plain != want {
		t.Fatalf("plain forward:\n got %q\nwant %q", plain, want)
	}
}

// The envelope is someone else's text going into our HTML, so it is escaped.
func TestRenderForwardedHTMLEscapesEnvelope(t *testing.T) {
	m := forwardedFixture()
	m.Subject = `<script>alert(1)</script>`
	htmlBody, _ := renderForwarded(m, time.UTC)

	if strings.Contains(htmlBody, "<script>") {
		t.Fatalf("subject reached the HTML unescaped: %s", htmlBody)
	}
	for _, want := range []string{
		"---------- Forwarded message ---------",
		"From: John Smith &lt;john@example.com&gt;",
		"Date: Wed, Sep 23, 2026 at 8:34 AM UTC",
		"Cc: finance@example.com",
		"Here are the pricing details...",
	} {
		if !strings.Contains(htmlBody, want) {
			t.Fatalf("HTML forward is missing %q:\n%s", want, htmlBody)
		}
	}
}

// The original's HTML survives, sanitized, with its stylesheet folded inline.
func TestRenderForwardedKeepsSanitizedHTML(t *testing.T) {
	m := forwardedFixture()
	m.BodyHTML = `<html><head><style>.lead{color:#ff0000}</style></head><body bgcolor="#000000">` +
		`<p class="lead">Quarterly <b>pricing</b></p><img src="x" onerror="alert(1)"><script>steal()</script></body></html>`
	htmlBody, plain := renderForwarded(m, time.UTC)

	if !strings.Contains(htmlBody, "<b>pricing</b>") {
		t.Fatalf("original markup was flattened:\n%s", htmlBody)
	}
	if !strings.Contains(htmlBody, "color:#ff0000") && !strings.Contains(htmlBody, "color: #ff0000") {
		t.Fatalf("original stylesheet was lost instead of inlined:\n%s", htmlBody)
	}
	for _, bad := range []string{"onerror", "<script", "steal()", "<style", "<body", "</body"} {
		if strings.Contains(htmlBody, bad) {
			t.Fatalf("unsafe markup %q survived:\n%s", bad, htmlBody)
		}
	}
	if !strings.Contains(plain, "Here are the pricing details...") {
		t.Fatalf("stored plain text should be the text part:\n%s", plain)
	}
}

func TestRenderForwardedFallsBackBetweenParts(t *testing.T) {
	m := forwardedFixture()
	m.BodyPlain = ""
	m.BodyHTML = `<p>Only <b>HTML</b> here</p>`
	_, plain := renderForwarded(m, time.UTC)
	if !strings.HasSuffix(plain, "Only HTML here") {
		t.Fatalf("plain part should be derived from the HTML:\n%q", plain)
	}

	m = forwardedFixture()
	m.CC = nil
	htmlBody, plain := renderForwarded(m, time.UTC)
	if !strings.Contains(htmlBody, "Here are the pricing details...") {
		t.Fatalf("HTML part should be derived from the text:\n%s", htmlBody)
	}
	if strings.Contains(plain, "Cc:") {
		t.Fatalf("an empty Cc should not be listed:\n%s", plain)
	}
}

func TestForwardNoteFillsTheMissingPart(t *testing.T) {
	h, p := forwardNote("", "FYI")
	if !strings.Contains(h, "FYI") || p != "FYI" {
		t.Fatalf("plain-only note: html %q plain %q", h, p)
	}
	h, p = forwardNote("<div>Take a <b>look</b></div>", "")
	if h != "<div>Take a <b>look</b></div>" || p != "Take a look" {
		t.Fatalf("html-only note: html %q plain %q", h, p)
	}
	h, p = forwardNote("", "")
	if h != "" || p != "" {
		t.Fatalf("an empty note must stay empty: html %q plain %q", h, p)
	}
}

type fakeSendEmailRepo struct {
	repository.EmailRepository
	account *models.Email
}

func (f *fakeSendEmailRepo) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return f.account, nil
}

type fakeSendTaskRepo struct {
	repository.TaskRepository
	stored *repository.EmailTask
}

func (f *fakeSendTaskRepo) CreateEmailTaskFull(_ context.Context, _ *repository.Task, et *repository.EmailTask) error {
	f.stored = et
	return nil
}

// The queued task carries the rendered message beside the note, in the
// mailbox's own time zone, and the note gains the part it was sent without.
func TestSendEmailStoresTheForwardedMessage(t *testing.T) {
	orgID := uuid.New()
	tasks := &fakeSendTaskRepo{}
	svc := &emailSendService{
		taskRepo:  tasks,
		emailRepo: &fakeSendEmailRepo{account: &models.Email{OrganizationID: &orgID, Timezone: "America/New_York"}},
	}

	_, xerr := svc.SendEmail(context.Background(), uuid.New(), orgID, uuid.New(), &SendEmailRequest{
		To: []string{"new@example.com"}, Subject: "Fwd: Pricing details", BodyPlain: "FYI",
		Forward: forwardedFixture(),
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	et := tasks.stored
	if et == nil {
		t.Fatal("no task stored")
	}
	if et.BodyPlain != "FYI" || !strings.Contains(et.BodyHTML, "FYI") {
		t.Fatalf("note: plain %q html %q", et.BodyPlain, et.BodyHTML)
	}
	if !strings.Contains(et.ForwardedPlain, "Date: Wed, Sep 23, 2026 at 4:34 AM EDT") ||
		!strings.Contains(et.ForwardedHTML, "Here are the pricing details...") {
		t.Fatalf("forwarded message: plain %q html %q", et.ForwardedPlain, et.ForwardedHTML)
	}

	// A plain reply stores no forward and keeps its body exactly.
	_, xerr = svc.SendEmail(context.Background(), uuid.New(), orgID, uuid.New(), &SendEmailRequest{
		To: []string{"them@example.com"}, Subject: "Re: x", BodyPlain: "Thanks",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if et := tasks.stored; et.ForwardedHTML != "" || et.ForwardedPlain != "" || et.BodyHTML != "" || et.BodyPlain != "Thanks" {
		t.Fatalf("reply changed: %+v", et)
	}
}
