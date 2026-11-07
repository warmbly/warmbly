package wmail

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

// A refreshed token says what Google granted, so one without full mail access never dials.
func TestGmailIMAPRefusesATokenWithoutFullMailAccess(t *testing.T) {
	tok := (&oauth2.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}).WithExtra(map[string]any{
		"scope": "https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/gmail.settings.basic",
	})
	g := NewGmailIMAP(context.Background(), "owner"+"@example.com", oauth2.StaticTokenSource(tok))
	err := g.With(context.Background(), func(GmailIMAPSession) error {
		t.Fatal("a session opened for a token without full mail access")
		return nil
	})
	if !errors.Is(err, ErrGmailIMAPScope) {
		t.Fatalf("With = %v, want ErrGmailIMAPScope", err)
	}
}

type failingTokens struct{ err error }

func (f failingTokens) Token() (*oauth2.Token, error) { return nil, f.err }

// A control plane refusing the IMAP token is standing; a token fetch that failed otherwise is not.
func TestGmailIMAPReadsABrokerRefusalAsMissingScope(t *testing.T) {
	for err, want := range map[error]bool{
		&repository.BrokeredTokenRefused{Code: "google_delegation_unauthorized", Message: "entry lacks the scope"}: true,
		errors.New("brokered_token.http: status 503: provider down"):                                               false,
	} {
		g := NewGmailIMAP(context.Background(), "owner"+"@example.com", failingTokens{err})
		got := g.With(context.Background(), func(GmailIMAPSession) error { return nil })
		if errors.Is(got, ErrGmailIMAPScope) != want {
			t.Fatalf("token error %v dialed to %v", err, got)
		}
	}
}
