package emailsend

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// The whole point of the opt-in is that mail goes out clean unless somebody
// asked otherwise, so every "should not track" branch is worth a case: an
// accidental default here puts a pixel in someone's personal reply.
func TestApplyDirectTracking(t *testing.T) {
	t.Setenv("TRACKING_DOMAIN", "t.example.com")

	const body = `<html><body><p>hello</p><a href="https://example.com/docs">docs</a></body></html>`
	taskID := uuid.New()
	svc := &emailSendService{}

	cases := []struct {
		name    string
		account *models.Email
		body    string
		want    bool
	}{
		{"opted out", &models.Email{TrackDirectMail: false}, body, false},
		{"opted in", &models.Email{TrackDirectMail: true}, body, true},
		{"no account", nil, body, false},
		{"plain-text only send", &models.Email{TrackDirectMail: true}, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, tracked := svc.applyDirectTracking(context.Background(), tc.account, taskID, tc.body, tc.body != "")
			if tracked != tc.want {
				t.Fatalf("tracked = %v, want %v", tracked, tc.want)
			}
			hasPixel := strings.Contains(got, "/t/o/"+taskID.String())
			if hasPixel != tc.want {
				t.Fatalf("pixel present = %v, want %v (body: %q)", hasPixel, tc.want, got)
			}
			if !tc.want && got != tc.body {
				t.Fatalf("body was modified for an untracked send:\n got %q\nwant %q", got, tc.body)
			}
		})
	}
}

// An install with no tracking host has nowhere to send a pixel, so opting in
// must still produce a clean message rather than a beacon pointing at nothing.
func TestApplyDirectTrackingWithoutHost(t *testing.T) {
	t.Setenv("TRACKING_DOMAIN", "")

	const body = `<html><body>hi</body></html>`
	svc := &emailSendService{}

	got, tracked := svc.applyDirectTracking(context.Background(), &models.Email{TrackDirectMail: true}, uuid.New(), body, true)
	if tracked {
		t.Fatal("reported tracked with no tracking host configured")
	}
	if got != body {
		t.Fatalf("body was modified with no tracking host:\n got %q\nwant %q", got, body)
	}
}

// Links are only wrapped when there is somewhere to store the tickets. Without
// the repository the pixel still goes on, because it needs no row to resolve.
func TestApplyDirectTrackingWithoutLinkStore(t *testing.T) {
	t.Setenv("TRACKING_DOMAIN", "t.example.com")

	const body = `<html><body><a href="https://example.com/docs">docs</a></body></html>`
	svc := &emailSendService{}

	got, tracked := svc.applyDirectTracking(context.Background(), &models.Email{TrackDirectMail: true}, uuid.New(), body, true)
	if !tracked {
		t.Fatal("expected the pixel to be applied without a link store")
	}
	if !strings.Contains(got, "https://example.com/docs") {
		t.Fatalf("original link did not survive:\n%s", got)
	}
}

// A forward always ships an HTML part, so a forward with no note is tracked too.
func TestApplyDirectTrackingForwardWithoutNote(t *testing.T) {
	t.Setenv("TRACKING_DOMAIN", "t.example.com")

	taskID := uuid.New()
	got, tracked := (&emailSendService{}).applyDirectTracking(context.Background(), &models.Email{TrackDirectMail: true}, taskID, "", true)
	if !tracked || !strings.Contains(got, "/t/o/"+taskID.String()) {
		t.Fatalf("empty-note forward was not tracked: tracked=%v body=%q", tracked, got)
	}
}
