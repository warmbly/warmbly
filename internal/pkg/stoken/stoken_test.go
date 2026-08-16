package stoken

import (
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type staticSource struct {
	token *oauth2.Token
}

func (s *staticSource) Token() (*oauth2.Token, error) {
	return s.token, nil
}

func TestTokenCallsOnUpdate(t *testing.T) {
	src := &staticSource{token: &oauth2.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}}

	var got *oauth2.Token
	ts := New(src, func(tok *oauth2.Token) error {
		got = tok
		return nil
	})

	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "a" {
		t.Errorf("got access token %q, want %q", tok.AccessToken, "a")
	}
	if got == nil || got.AccessToken != "a" {
		t.Error("onUpdate was not called with the refreshed token")
	}
}

// The oauth2 transport calls Token() on every request, so a nil callback used
// to panic inside RoundTrip on the first API call the client made.
func TestTokenWithNilOnUpdateDoesNotPanic(t *testing.T) {
	src := &staticSource{token: &oauth2.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}}
	ts := New(src, nil)

	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "a" {
		t.Errorf("got access token %q, want %q", tok.AccessToken, "a")
	}
}
