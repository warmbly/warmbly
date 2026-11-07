package mailpush

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"google.golang.org/api/idtoken"
)

// fakeGmailPush is a GmailPush that accepts tokens without contacting Google.
func fakeGmailPush(claims map[string]any, issuer string) *GmailPush {
	return &GmailPush{audience: "https://api.example.com" + GmailPushPath, account: "push@p.iam.gserviceaccount.com",
		validate: func(_ context.Context, token, audience string) (*idtoken.Payload, error) {
			if token != "good" || audience != "https://api.example.com"+GmailPushPath {
				return nil, errors.New("bad token")
			}
			return &idtoken.Payload{Issuer: issuer, Claims: claims}, nil
		}}
}

func TestGmailPushVerify(t *testing.T) {
	valid := map[string]any{"email": "push@p.iam.gserviceaccount.com", "email_verified": true}
	for _, tc := range []struct {
		name   string
		header string
		claims map[string]any
		issuer string
		ok     bool
	}{
		{"signed by the push account", "Bearer good", valid, "https://accounts.google.com", true},
		{"no bearer", "good", valid, "https://accounts.google.com", false},
		{"invalid signature", "Bearer forged", valid, "https://accounts.google.com", false},
		{"another account", "Bearer good", map[string]any{"email": "other@p.iam.gserviceaccount.com", "email_verified": true}, "accounts.google.com", false},
		{"unverified email", "Bearer good", map[string]any{"email": "push@p.iam.gserviceaccount.com"}, "accounts.google.com", false},
		{"foreign issuer", "Bearer good", valid, "https://evil.example", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fakeGmailPush(tc.claims, tc.issuer).Verify(t.Context(), tc.header)
			if (err == nil) != tc.ok {
				t.Fatalf("Verify = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestGmailPushAddress(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte(`{"emailAddress":"a@b.com","historyId":"9"}`))
	if got, ok := GmailPushAddress([]byte(`{"message":{"data":"` + data + `","messageId":"1"},"subscription":"s"}`)); !ok || got != "a@b.com" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := GmailPushAddress([]byte(`{"message":{"data":"!!"}}`)); ok {
		t.Fatal("undecodable data parsed")
	}
}

func TestGmailPushNeedsAccountAndHTTPS(t *testing.T) {
	t.Setenv("BACKEND_PUBLIC_URL", "https://api.example.com")
	t.Setenv("GMAIL_PUSH_SERVICE_ACCOUNT", "")
	if NewGmailPush() != nil {
		t.Fatal("push accepted without a service account to check")
	}
	t.Setenv("GMAIL_PUSH_SERVICE_ACCOUNT", "push@p.iam.gserviceaccount.com")
	t.Setenv("BACKEND_PUBLIC_URL", "http://localhost:8080")
	if NewGmailPush() != nil {
		t.Fatal("Pub/Sub pushes only to HTTPS")
	}
}
