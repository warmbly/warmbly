package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
)

type fakeTableRetention struct {
	before map[string]time.Time
	fail   string
}

func (f *fakeTableRetention) record(table string, before time.Time) (int64, error) {
	f.before[table] = before
	if table == f.fail {
		return 0, errors.New("boom")
	}
	return 1, nil
}

func (f *fakeTableRetention) PruneCampaignLogs(_ context.Context, b time.Time) (int64, error) {
	return f.record("campaign_logs", b)
}
func (f *fakeTableRetention) PruneNotifications(_ context.Context, b time.Time) (int64, error) {
	return f.record("notifications", b)
}
func (f *fakeTableRetention) PruneWebhookDeliveries(_ context.Context, b time.Time) (int64, error) {
	return f.record("webhook_deliveries", b)
}
func (f *fakeTableRetention) PruneWorkerHealthSamples(_ context.Context, b time.Time) (int64, error) {
	return f.record("worker_health_samples", b)
}
func (f *fakeTableRetention) PruneDeliveredSendResultEffects(_ context.Context, b time.Time) (int64, error) {
	return f.record("send_result_effects", b)
}

type fixedRetention instancesettings.Retention

func (r fixedRetention) RetentionWindows(context.Context) instancesettings.Retention {
	return instancesettings.Retention(r)
}

func TestTableRetentionReadsWindowsAndSurvivesAFailingTable(t *testing.T) {
	store := &fakeTableRetention{before: map[string]time.Time{}, fail: "notifications"}
	windows := instancesettings.DefaultRetention()
	windows.CampaignLogDays, windows.NotificationDays, windows.WebhookDeliveryDays = 10, 15, 20
	err := NewTableRetentionJob(store, fixedRetention(windows)).Run(context.Background())
	if err == nil {
		t.Fatal("a failing table was not reported")
	}
	if len(store.before) != 5 {
		t.Fatalf("pruned %d tables, want all five despite the failure: %v", len(store.before), store.before)
	}
	for table, days := range map[string]int{
		"campaign_logs": 10, "webhook_deliveries": 20,
		"worker_health_samples": workerHealthSampleKeepDays, "send_result_effects": sendResultEffectKeepDays,
	} {
		age := time.Since(store.before[table])
		if want := time.Duration(days) * 24 * time.Hour; age < want-2*time.Hour || age > want+2*time.Hour {
			t.Errorf("%s pruned before %v ago, want %d days", table, age, days)
		}
	}
}

// An instance that never chose a feed window keeps its feeds; only bookkeeping is pruned.
func TestTableRetentionKeepsFeedsByDefault(t *testing.T) {
	store := &fakeTableRetention{before: map[string]time.Time{}}
	if err := NewTableRetentionJob(store, fixedRetention(instancesettings.DefaultRetention())).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, feed := range []string{"campaign_logs", "notifications", "webhook_deliveries"} {
		if _, pruned := store.before[feed]; pruned {
			t.Errorf("%s was pruned with no window set", feed)
		}
	}
	if len(store.before) != 2 {
		t.Fatalf("pruned %v, want only the worker health samples and delivered effects", store.before)
	}
}
