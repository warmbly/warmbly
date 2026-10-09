package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailclient"
)

// How a contact reads (issue #655): each person's open carries its client,
// app or webmail, and whether a proxy hid the device, and every surface that
// reads the open log hands those back.
func TestLiveEngagementOriginReachesEverySurface(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 201)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	step := uuid.New()
	task := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql[:min(50, len(sql))], err)
		}
	}
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html)
	      VALUES ($1, $2, $3, 'Intro', 'Quick question', '', '')`, step, f.campaign, f.org)
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, opened_at, opened_machine, clicked_at)
	      VALUES ($1, $2, $3, $4, $5, false, $6)`, f.campaign, f.contact, step, now.Add(-3*time.Hour), now.Add(-2*time.Hour), now.Add(-time.Minute))

	open := func(at time.Time, machine bool, reason, client, clientType string, hidden bool, device, os string) {
		t.Helper()
		exec(`INSERT INTO email_opens (id, task_id, campaign_id, contact_id, sequence_id, opened_at, machine, machine_reason,
		                               client, client_type, device_hidden, device_type, os, country_code, city)
		      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'DE', 'Berlin')`,
			uuid.New(), task, f.campaign, f.contact, step, at, machine, reason, client, clientType, hidden, device, os)
	}
	open(now.Add(-2*time.Hour), false, "", "Apple Mail", models.EngagementClientApp, false, "mobile", "iOS")
	open(now.Add(-90*time.Minute), false, "", "Gmail", "", true, "", "")
	open(now.Add(-30*time.Minute), false, "", "Apple Mail", models.EngagementClientApp, false, "mobile", "iOS")
	// The scanner reason is one the open log must accept.
	open(now.Add(-10*time.Minute), true, EmailOpenReasonScanner, "", "", false, "desktop", "Windows")
	exec(`INSERT INTO email_link_clicks (task_id, campaign_id, contact_id, sequence_id, destination, clicked_at,
	                                     device_type, os, browser, machine, machine_reason)
	      VALUES ($1, $2, $3, $4, 'https://example.com', $5, 'mobile', 'iOS', 'Safari', false, '')`,
		task, f.campaign, f.contact, step, now.Add(-time.Minute))
	exec(`INSERT INTO email_link_clicks (task_id, campaign_id, contact_id, sequence_id, destination, clicked_at, machine, machine_reason)
	      VALUES ($1, $2, $3, $4, 'https://example.com/b', $5, true, 'scanner')`,
		task, f.campaign, f.contact, step, now.Add(-2*time.Minute))

	contacts := NewContactRepostory(handle)
	detail, xerr := contacts.GetDetail(ctx, f.owner, &f.org, f.contact)
	if xerr != nil {
		t.Fatalf("detail: %v", xerr)
	}
	reads := detail.Engagement.ReadsOn
	if len(reads) != 2 {
		t.Fatalf("reads_on has %d rows, want the iPhone and Gmail (machine opens left out): %+v", len(reads), reads)
	}
	if r := reads[0]; r.Client != "Apple Mail" || r.ClientType != models.EngagementClientApp || r.DeviceType != "mobile" || r.Opens != 2 {
		t.Fatalf("most recent reading = %+v, want Apple Mail app on mobile with 2 opens", r)
	}
	if r := reads[1]; r.Client != "Gmail" || !r.DeviceHidden || r.Opens != 1 {
		t.Fatalf("second reading = %+v, want Gmail with the device hidden", r)
	}
	if _, xerr := contacts.GetDetail(ctx, f.owner, &f.org, uuid.New()); xerr == nil {
		t.Fatal("an unknown contact has no detail")
	}

	res, xerr := contacts.ListTimeline(ctx, f.org, f.contact, 50, nil)
	if xerr != nil {
		t.Fatalf("timeline: %v", xerr)
	}
	var hidden, app int
	for _, ev := range res.Data {
		if ev.Type != models.TimelineEmailOpened || ev.Origin == nil {
			continue
		}
		if ev.Origin.DeviceHidden {
			hidden++
		}
		if ev.Origin.ClientType == models.EngagementClientApp {
			app++
		}
	}
	if hidden != 1 || app != 2 {
		t.Fatalf("timeline opens carry %d hidden and %d app origins, want 1 and 2", hidden, app)
	}

	analytics := NewAnalyticsRepository(handle)
	recent, xerr := analytics.GetRecentActivity(ctx, f.org, 20, nil)
	if xerr != nil {
		t.Fatalf("recent activity: %v", xerr)
	}
	var sawOpen, sawClick bool
	for _, a := range recent {
		switch a.Type {
		case "opened":
			sawOpen = true
			if a.Origin == nil || a.Origin.Client != "Apple Mail" || a.Origin.DeviceType != "mobile" {
				t.Fatalf("recent open origin = %+v, want the first person's open (Apple Mail on mobile)", a.Origin)
			}
		case "clicked":
			sawClick = true
			if a.Origin == nil || a.Origin.Browser != "Safari" {
				t.Fatalf("recent click origin = %+v, want the person's Safari click, not the scanner's", a.Origin)
			}
		}
	}
	if !sawOpen || !sawClick {
		t.Fatalf("recent activity missed the open or the click: %+v", recent)
	}

	breakdown, xerr := analytics.GetCampaignEngagementBreakdown(ctx, f.campaign, nil, 8)
	if xerr != nil {
		t.Fatalf("breakdown: %v", xerr)
	}
	surfaces := map[string]models.EngagementBucket{}
	for _, b := range breakdown.Surfaces {
		surfaces[b.Key] = b
	}
	if surfaces["mobile_app"].Opens != 1 || surfaces[models.EngagementSurfaceHidden].Opens != 1 || surfaces["mobile"].Clicks != 1 {
		t.Fatalf("surfaces = %+v, want one mobile_app open, one hidden open and one mobile click", breakdown.Surfaces)
	}
}

// Rows logged before the current rules are re-read in id-ordered batches,
// clicks after opens, and the walk records that it finished.
func TestLiveOriginBackfillRereadsStoredRows(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 201)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	step := uuid.New()
	if _, err := pool.Exec(ctx, `DELETE FROM admin_settings WHERE key = $1`, originBackfillKey); err != nil {
		t.Fatalf("reset progress: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM admin_settings WHERE key = $1`, originBackfillKey)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html)
	      VALUES ($1, $2, $3, 'Intro', 'Hi', '', '')`, step, f.campaign, f.org); err != nil {
		t.Fatalf("seed step: %v", err)
	}
	gmail := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO email_opens (id, task_id, campaign_id, contact_id, sequence_id, opened_at, user_agent,
	                               client, device_type, os, browser, browser_version, country_code, region, city)
	      VALUES ($1, $2, $3, $4, $5, NOW(), 'Mozilla/5.0 (Windows NT 5.1; rv:11.0) Gecko Firefox/11.0 (via ggpht.com GoogleImageProxy)',
	              'Gmail', 'desktop', 'Windows', 'Firefox', '11.0', 'US', 'California', 'Mountain View')`,
		gmail, uuid.New(), f.campaign, f.contact, step); err != nil {
		t.Fatalf("seed open: %v", err)
	}
	click := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO email_link_clicks (id, task_id, campaign_id, contact_id, sequence_id, destination, user_agent,
	                                     client, os, browser)
	      VALUES ($1, $2, $3, $4, $5, 'https://x', 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko)',
	              'Apple Mail', 'macOS', 'Intel Mac OS X 10_15_7')`,
		click, uuid.New(), f.campaign, f.contact, step); err != nil {
		t.Fatalf("seed click: %v", err)
	}

	derive := func(ua string, click bool, stored models.EngagementOrigin) models.EngagementOrigin {
		r := mailclient.Detect(ua, click)
		o := models.EngagementOrigin{Client: r.Client, ClientType: r.ClientType, DeviceHidden: r.DeviceHidden,
			DeviceType: r.DeviceType, OS: r.OS, Browser: r.Browser, BrowserVersion: r.BrowserVersion}
		if r.Locate == mailclient.LocateFull {
			o.CountryCode, o.Region, o.City = stored.CountryCode, stored.Region, stored.City
		}
		return o
	}

	// Another consumer holding the lock makes this one wait, not race.
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, originBackfillLock); err != nil {
		t.Fatalf("lock: %v", err)
	}
	repo := NewEmailOpenRepository(pool)
	if done, busy, err := repo.BackfillOriginBatch(ctx, 500, derive); err != nil || done || !busy {
		t.Fatalf("with the lock held: done=%v busy=%v err=%v, want busy", done, busy, err)
	}
	_ = holder.Rollback(ctx)

	for i := 0; ; i++ {
		done, busy, err := repo.BackfillOriginBatch(ctx, 500, derive)
		if err != nil || busy {
			t.Fatalf("batch %d: busy=%v err=%v", i, busy, err)
		}
		if done {
			break
		}
		if i > 10000 {
			t.Fatal("backfill never finished")
		}
	}

	var client, device, os, city string
	var hidden bool
	if err := pool.QueryRow(ctx, `SELECT client, device_hidden, device_type, os, city FROM email_opens WHERE id = $1`, gmail).
		Scan(&client, &hidden, &device, &os, &city); err != nil {
		t.Fatalf("read open: %v", err)
	}
	if client != "Gmail" || !hidden || device != "" || os != "" || city != "" {
		t.Fatalf("gmail open after backfill = %q hidden=%v %q %q %q", client, hidden, device, os, city)
	}
	var browser string
	if err := pool.QueryRow(ctx, `SELECT client, os, browser FROM email_link_clicks WHERE id = $1`, click).Scan(&client, &os, &browser); err != nil {
		t.Fatalf("read click: %v", err)
	}
	if client != "" || os != "" || browser != "" {
		t.Fatalf("bare webkit click after backfill = %q %q %q, want nothing claimed", client, os, browser)
	}
	if done, _, err := repo.BackfillOriginBatch(ctx, 500, derive); err != nil || !done {
		t.Fatalf("a finished backfill answers done: done=%v err=%v", done, err)
	}
}
