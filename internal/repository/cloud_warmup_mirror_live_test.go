package repository

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveCloudWarmupMirrorWritesOnlyEnrolledMailboxesInOrder(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 314)
	ctx := context.Background()
	f := newSharedOrgFixture(t, pool)
	enrolled, leaving, local, instance := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO cloud_link (cloud_url, instance_id, token) VALUES ('https://cloud.example.invalid', $1, 'fixture')`, instance); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{enrolled, leaving, local}); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM cloud_link WHERE instance_id = $1`, instance); err != nil {
			t.Error(err)
		}
	})
	for _, id := range []uuid.UUID{enrolled, leaving, local} {
		if _, err := pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status, timezone, warmup_days)
			VALUES ($1, $2, $3, $4, 'Mirror', '', '', 'smtp_imap', 'active', 'Europe/Berlin', 31)`, id, f.owner, f.org, uuid.NewString()+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewCloudLinkRepository(pool, nil, testSessionConnector(pool))
	for _, id := range []uuid.UUID{enrolled, leaving} {
		if _, err := repo.Enroll(ctx, id, id, instance, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE cloud_link_mailboxes SET enrollment_state = 'pending_remove' WHERE email_account_id = $1`, leaving); err != nil {
		t.Fatal(err)
	}
	mirror := repo.(CloudWarmupMirrorRepository)
	cloud := models.PoolLinkWarmupSettings{Base: 12, Max: 35, Increase: 2, ReplyRate: 20, StartTime: "09:00:00", EndTime: "18:30:00", Days: models.WarmupEveryDay, Timezone: "UTC"}
	all := map[uuid.UUID]models.PoolLinkWarmupSettings{enrolled: cloud, leaving: cloud, local: cloud}
	read := func(id uuid.UUID) (base, days int, start, tz string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT warmup_base, warmup_days, to_char(warmup_start_time, 'HH24:MI'), timezone FROM email_accounts WHERE id = $1`, id).Scan(&base, &days, &start, &tz); err != nil {
			t.Fatal(err)
		}
		return
	}

	first := time.Now()
	changed, err := mirror.MirrorCloudWarmupSettings(ctx, first, all)
	if err != nil || !slices.Equal(changed, []uuid.UUID{enrolled}) {
		t.Fatalf("changed = %v, %v, want only the enrolled mailbox", changed, err)
	}
	if base, days, start, tz := read(enrolled); base != 12 || days != models.WarmupEveryDay || start != "09:00" || tz != "Europe/Berlin" {
		t.Fatalf("enrolled mailbox = base %d days %d start %s tz %s, want the cloud's ramp, explicit every day, our timezone", base, days, start, tz)
	}
	for _, id := range []uuid.UUID{leaving, local} {
		if base, days, _, _ := read(id); base != 10 || days != 31 {
			t.Fatalf("mailbox %v was mirrored: base %d days %d", id, base, days)
		}
	}

	if changed, err := mirror.MirrorCloudWarmupSettings(ctx, first.Add(time.Minute), all); err != nil || len(changed) != 0 {
		t.Fatalf("an unchanged report changed %v (%v)", changed, err)
	}
	stale := cloud
	stale.Base = 30
	if changed, err := mirror.MirrorCloudWarmupSettings(ctx, first, map[uuid.UUID]models.PoolLinkWarmupSettings{enrolled: stale}); err != nil || len(changed) != 0 {
		t.Fatalf("an older observation overwrote a newer one: %v (%v)", changed, err)
	}
	broken := cloud
	broken.Max = 500
	if changed, err := mirror.MirrorCloudWarmupSettings(ctx, first.Add(time.Hour), map[uuid.UUID]models.PoolLinkWarmupSettings{enrolled: broken}); err != nil || len(changed) != 0 {
		t.Fatalf("an out-of-range report was written: %v (%v)", changed, err)
	}
	m, err := repo.GetByAccount(ctx, enrolled)
	if err != nil || m.SettingsObservedAt == nil || m.SettingsObservedAt.Before(first.Add(time.Minute).Add(-time.Second)) {
		t.Fatalf("observed stamp = %+v (%v), want the newest accepted observation", m, err)
	}
}
