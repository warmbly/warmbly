package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveCampaignLoadingLogsKeepHistoryWithoutCustomerAlerts(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignLogRepository(handle)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM campaign_logs WHERE campaign_id=$1`, f.campaign) })
	for _, tc := range []struct {
		name   string
		code   string
		retry  bool
		text   string
		marker bool
	}{
		{name: "confirmed retry", code: models.MailboxLoadingCode, retry: true, text: models.MailboxNotLoadedPrefix, marker: true},
		{name: "unconfirmed loading retry", code: string(errx.MailErrorCodeServerUnreachable), retry: true, text: models.MailboxNotLoadedPrefix},
		{name: "exhausted", code: string(errx.MailErrorCodeServerUnreachable), text: models.MailboxNotLoadedPrefix},
		{name: "provider failure", code: string(errx.MailErrorCodeServerUnreachable), retry: true, text: "provider connection failed"},
		{name: "auth failure", code: string(errx.MailErrorCodeInvalidCredentials), retry: true, text: models.MailboxNotLoadedPrefix},
	} {
		if err := repo.CreateLog(ctx, &CampaignLogEntry{CampaignID: f.campaign, EventType: "email_failed", Message: tc.name, Metadata: map[string]interface{}{
			"level": "error", "code": tc.code, "error": tc.text, "will_retry": tc.retry, "mailbox_loading_retry": tc.marker,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := repo.GetLogs(ctx, f.campaign, 100, nil)
	if err != nil || len(logs.Data) != 5 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	normalized, delayed := 0, 0
	for _, entry := range logs.Data {
		switch {
		case entry.Message == "Sending was delayed while the mailbox reconnected. An automatic retry was scheduled.":
			normalized++
			if entry.EventType != "email_deferred" || entry.Metadata["level"] != "info" {
				t.Fatalf("loading retry is still an alert: %+v", entry)
			}
		case strings.HasPrefix(entry.Message, "Sending to the recipient was delayed because the mail provider did not respond"):
			// A retried temporary provider failure is a delay, not an alert.
			delayed++
			if entry.EventType != "email_deferred" || entry.Metadata["level"] != "info" {
				t.Fatalf("temporary provider retry is still an alert: %+v", entry)
			}
		case entry.EventType != "email_failed" || entry.Metadata["level"] != "error":
			t.Fatalf("real or exhausted failure was hidden: %+v", entry)
		}
	}
	if normalized != 1 || delayed != 1 {
		t.Fatalf("normalized %d loading retries and %d provider retries, want one each", normalized, delayed)
	}
	var raw int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_logs WHERE campaign_id=$1 AND event_type='email_failed' AND metadata->>'level'='error'`, f.campaign).Scan(&raw); err != nil || raw != 5 {
		t.Fatalf("historical diagnostics changed: raw=%d err=%v", raw, err)
	}
}
