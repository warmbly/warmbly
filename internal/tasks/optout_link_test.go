package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeUnsubTickets struct {
	err    error
	stored string
	calls  int
}

func (f *fakeUnsubTickets) Mint(_ context.Context, token string, _, _, _ uuid.UUID, _ time.Time) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	if f.stored != "" {
		return f.stored, nil
	}
	return token, nil
}

func (f *fakeUnsubTickets) Resolve(context.Context, string) (*repository.UnsubscribeTicket, error) {
	return nil, nil
}

func TestMintUnsubscribeLinkPrefersAStoredTicket(t *testing.T) {
	tickets := &fakeUnsubTickets{}
	s := &tasksService{
		unsubLinks:   unsublink.New("secret", "https://api.example.com"),
		unsubTickets: tickets,
	}

	got := s.mintUnsubscribeLink(context.Background(), "https://t.customer.com", uuid.New(), uuid.New(), uuid.New())

	if tickets.calls != 1 {
		t.Fatalf("minted %d tickets, want 1", tickets.calls)
	}
	token := strings.TrimPrefix(got, "https://t.customer.com"+unsublink.Path)
	if token == got || !unsublink.IsTicket(token) {
		t.Fatalf("not a ticket link: %q", got)
	}
}

// A follow-up reuses the address the first email carried, so a recipient who
// kept the first message and clicks its link months later reaches the same
// place as one clicking the latest.
func TestMintUnsubscribeLinkReturnsTheStoredToken(t *testing.T) {
	tickets := &fakeUnsubTickets{stored: "firstsend0000000000000"}
	s := &tasksService{
		unsubLinks:   unsublink.New("secret", "https://api.example.com"),
		unsubTickets: tickets,
	}

	got := s.mintUnsubscribeLink(context.Background(), "", uuid.New(), uuid.New(), uuid.New())

	if want := "https://api.example.com" + unsublink.Path + "firstsend0000000000000"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The store failing must cost the recipient a long address, never the opt-out
// itself: a signed link needs no row and works the same.
func TestMintUnsubscribeLinkFallsBackToTheSignedLink(t *testing.T) {
	signer := unsublink.New("secret", "https://api.example.com")
	s := &tasksService{
		unsubLinks:   signer,
		unsubTickets: &fakeUnsubTickets{err: errors.New("postgres is down")},
	}
	org, campaign, contact := uuid.New(), uuid.New(), uuid.New()

	got := s.mintUnsubscribeLink(context.Background(), "", org, campaign, contact)

	token := strings.TrimPrefix(got, "https://api.example.com"+unsublink.Path)
	if token == got {
		t.Fatalf("unexpected link %q", got)
	}
	claims, err := signer.Verify(token, time.Now())
	if err != nil {
		t.Fatalf("the fallback link does not verify: %v", err)
	}
	if claims.OrgID != org || claims.CampaignID != campaign || claims.ContactID != contact {
		t.Fatalf("the fallback link names the wrong recipient: %+v", claims)
	}
}

// No store at all (a deployment wired without one) is the same fallback.
func TestMintUnsubscribeLinkWithoutAStore(t *testing.T) {
	s := &tasksService{unsubLinks: unsublink.New("secret", "https://api.example.com")}

	got := s.mintUnsubscribeLink(context.Background(), "", uuid.New(), uuid.New(), uuid.New())

	if token := strings.TrimPrefix(got, "https://api.example.com"+unsublink.Path); unsublink.IsTicket(token) {
		t.Fatalf("minted a ticket with no store: %q", got)
	}
}

// visibleText is the HTML with every tag removed: what a reader sees, as
// opposed to what an href holds. mailhtml.ToPlainText is not usable here
// because it deliberately renders a link as "label (url)".
func visibleText(html string) string {
	var b strings.Builder
	depth := 0
	for _, r := range html {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// The HTML part must never show the opt-out address as text. Issue #498 was
// reported against the plain-text alternative, where an address has nowhere
// to hide; this is the guard that the HTML half never regresses into the same
// thing, in each of the three shapes a step can carry an opt-out.
func TestHTMLEmailNeverShowsTheOptOutAddressAsText(t *testing.T) {
	const url = "https://t.acme.com/unsubscribe/Xk3mP9qR2tLwAb7dEfGhIj"
	link := models.UnsubscribeSettings{Mode: models.UnsubscribeModeLink}.Effective("")
	account := &models.Email{SignatureSync: true, SignatureHTML: "<p>Karan</p>", SignaturePlain: "Karan"}

	for _, tc := range []struct {
		name, body, wantLabel string
	}{
		{
			name:      "footer only",
			body:      `<div><p>I ended up trying the idea I had for your ad.</p></div>`,
			wantLabel: "Unsubscribe",
		},
		{
			name:      "the variable chip the composer saves",
			body:      `<div><p>Not interested? <span data-var="unsubscribe_link">{{.UnsubscribeLink}}</span></p></div>`,
			wantLabel: "Unsubscribe",
		},
		{
			name:      "the author's own wording around the variable",
			body:      `<div><p><a href="{{.UnsubscribeLink}}">stop these emails</a></p></div>`,
			wantLabel: "stop these emails",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered := previewTemplatesWith("Quick question", tc.body, "", models.Contact{}, url)
			bodyHTML, bodyPlain := finishBody(rendered.BodyHTML, rendered.BodyPlain, false, account, &link, url)

			if !strings.Contains(bodyHTML, `href="`+url+`"`) {
				t.Fatalf("the opt-out link is missing from the HTML part:\n%s", bodyHTML)
			}
			if seen := visibleText(bodyHTML); strings.Contains(seen, url) {
				t.Errorf("the address is visible text in the HTML part (issue #498):\n%s\n\nvisible: %s", bodyHTML, seen)
			}
			if !strings.Contains(visibleText(bodyHTML), tc.wantLabel) {
				t.Errorf("the reader has no %q to click:\n%s", tc.wantLabel, bodyHTML)
			}

			// The plain-text alternative is the one place the address is
			// printed, because text/plain has no anchor to put a word in.
			if !strings.Contains(bodyPlain, url) {
				t.Errorf("the plain part left the recipient no way out:\n%s", bodyPlain)
			}
		})
	}
}

// A plain-text campaign is the reported case and stays as it is: no HTML part
// at all, and the address in the copy, because there is nothing else it could
// be.
func TestPlainTextCampaignPrintsTheAddress(t *testing.T) {
	const url = "https://t.acme.com/unsubscribe/Xk3mP9qR2tLwAb7dEfGhIj"
	link := models.UnsubscribeSettings{Mode: models.UnsubscribeModeLink}.Effective("")

	bodyHTML, bodyPlain := finishBody(`<div><p>Quick one.</p></div>`, "", true, nil, &link, url)

	if bodyHTML != "" {
		t.Errorf("a plain-text campaign shipped an HTML part: %s", bodyHTML)
	}
	if !strings.Contains(bodyPlain, "Unsubscribe: "+url) {
		t.Errorf("plain-text opt-out:\n%s", bodyPlain)
	}
}
