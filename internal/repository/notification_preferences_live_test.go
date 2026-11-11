package repository

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// A save from a release that predates a category arrives without its key.
func TestLiveNotificationPreferencesKeepKeysASaveOmits(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	ctx := context.Background()
	notifs := NewNotificationRepository(handle.Pool)

	prefs := models.DefaultNotificationPreferences()
	prefs.InboundPositiveReply.Enabled = false
	if err := notifs.UpdatePreferences(ctx, f.user, &prefs); err != nil {
		t.Fatal(err)
	}
	older := `{"inbound_reply":{"enabled":true,"channels":{"in_app":true}},"email_digest_minutes":60}`
	if _, err := handle.Pool.Exec(ctx, `UPDATE users SET notification_preferences = $2::jsonb WHERE id = $1`, f.user, older); err != nil {
		t.Fatal(err)
	}
	got, err := notifs.GetPreferences(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if got.InboundPositiveReply.Enabled {
		t.Fatal("an omitted category lost its saved value")
	}
	if !got.InboundReply.Enabled || got.EmailDigestMinutes != 60 {
		t.Fatalf("the keys the save carried were not applied: %+v", got)
	}
	if !got.HealthBounce.Enabled {
		t.Fatal("an omitted category lost its saved value")
	}
}
