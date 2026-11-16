package repository

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func legacyDashboardOverallSQL(scope *models.CampaignScope, mailboxes []uuid.UUID) string {
	return `SELECT
		COUNT(CASE WHEN ccp.sent_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END),
		COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END),
		COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND ccp.opened_machine AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END),
		COUNT(CASE WHEN ccp.clicked_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END),
		COUNT(CASE WHEN ccp.clicked_at IS NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 AND EXISTS (
			SELECT 1 FROM email_link_clicks lc WHERE lc.campaign_id=ccp.campaign_id AND lc.contact_id=ccp.contact_id AND lc.sequence_id=ccp.sequence_id AND lc.machine
		) AND NOT EXISTS (
			SELECT 1 FROM email_link_clicks lc WHERE lc.campaign_id=ccp.campaign_id AND lc.contact_id=ccp.contact_id AND lc.sequence_id=ccp.sequence_id AND NOT lc.machine
		) THEN 1 END),
		COUNT(CASE WHEN ccp.replied_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END),
		COUNT(CASE WHEN ccp.bounced_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END),
		(SELECT COUNT(*) FROM campaigns ac WHERE ac.organization_id=$1 AND ac.status='active'` + inCampaignScope("ac.id", scope, 4) + `),
		(SELECT COUNT(*) FROM email_accounts WHERE organization_id=$1 AND status='active'` + inMailboxes("id", mailboxes, 4+len(scopeArgs(scope))) + `),
		COUNT(CASE WHEN ccp.sent_at >= $2 AND ccp.sent_at <= $3 AND COALESCE(ccp.open_tracked, c.open_tracking AND NOT c.text_only) THEN 1 END),
		COUNT(CASE WHEN ccp.sent_at >= $2 AND ccp.sent_at <= $3 AND COALESCE(ccp.click_tracked, c.link_tracking AND NOT c.text_only) THEN 1 END),
		COUNT(CASE WHEN ccp.sent_at >= $2 AND ccp.sent_at <= $3 AND ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine AND COALESCE(ccp.open_tracked, c.open_tracking AND NOT c.text_only) THEN 1 END),
		COUNT(CASE WHEN ccp.sent_at >= $2 AND ccp.sent_at <= $3 AND ccp.clicked_at IS NOT NULL AND COALESCE(ccp.click_tracked, c.link_tracking AND NOT c.text_only) THEN 1 END)
		FROM campaign_contact_progress ccp JOIN campaigns c ON c.id=ccp.campaign_id
		JOIN sequences s ON s.id=ccp.sequence_id AND s.kind='email'
		WHERE c.organization_id=$1` + inCampaignScope("c.id", scope, 4)
}

func TestLiveDashboardOverallBoundsHistoryAndPreservesScopedCounts(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	sf := newSharedOrgFixture(t, f.pool)
	foreign := newSharedOrgFixture(t, f.pool)
	repo := NewAnalyticsRepository(&db.DB{Pool: f.pool})
	from := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	sequence := uuid.New()
	for _, fixture := range []*sharedOrgFixture{sf, foreign} {
		step := sequence
		if fixture == foreign {
			step = uuid.New()
		}
		exec(`UPDATE campaigns SET status='active' WHERE organization_id=$1`, fixture.org)
		exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html)
			VALUES($1,$2,$3,'Report','Report','','')`, step, fixture.campaign, fixture.org)
		for i, sent := range []any{from.Add(-time.Microsecond), from, from.Add(time.Hour), to, to.Add(time.Microsecond), nil} {
			contact := addLead(t, fixture, uuid.NewString()+"@test.local", "valid", true)
			exec(`INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,sent_at,opened_at,opened_machine,clicked_at,replied_at,bounced_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$5,$5)`, fixture.campaign, contact, step, sent, to.Add(48*time.Hour), i%2 == 1,
				func() any {
					if i%2 == 0 {
						return to.Add(time.Hour)
					}
					return nil
				}())
			exec(`INSERT INTO email_link_clicks(task_id,campaign_id,contact_id,sequence_id,destination,clicked_at,machine)
				VALUES($1,$2,$3,$4,'https://test.local',$5,true)`, uuid.New(), fixture.campaign, contact, step, to.Add(time.Hour))
		}
	}
	// A send recorded as carrying tracked links counts in the click rate even unclicked.
	exec(`UPDATE campaign_contact_progress SET click_tracked = true WHERE campaign_id=$1 AND sent_at=$2`, sf.campaign, from)
	account := uuid.New()
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status)
		VALUES($1,$2,$3,$4,'Report','','','smtp_imap','active')`, account, sf.owner, sf.org, account.String()+"@test.local")
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id=$1`, account); err != nil {
			t.Error(err)
		}
	})
	action := uuid.New()
	exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,kind)
		VALUES($1,$2,$3,'Action','','','','action')`, action, sf.campaign, sf.org)
	exec(`INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,sent_at,opened_at)
		VALUES($1,$2,$3,$4,$4)`, sf.campaign, sf.contact, action, from)
	for _, window := range [][2]time.Time{{from, to}, {to.Add(24 * time.Hour), to.Add(48 * time.Hour)}, {to, from}, {from, from}} {
		for _, scope := range []*models.CampaignScope{nil, {CampaignIDs: []uuid.UUID{sf.campaign}}, {}, {CampaignIDs: []uuid.UUID{foreign.campaign}}} {
			for _, mailboxes := range [][]uuid.UUID{nil, {}, {account}, {f.sender}} {
				params := append(append([]any{sf.org, window[0], window[1]}, scopeArgs(scope)...), mailboxArgs(mailboxes)...)
				var want models.DashboardOverallStats
				var trackedOpens, trackedClicks int
				err := f.pool.QueryRow(ctx, legacyDashboardOverallSQL(scope, mailboxes), params...).Scan(&want.TotalEmailsSent,
					&want.TotalOpens, &want.MachineOpens, &want.TotalClicks, &want.MachineClicks, &want.TotalReplies,
					&want.TotalBounces, &want.ActiveCampaigns, &want.ActiveAccounts, &want.OpenTrackedSent, &want.ClickTrackedSent, &trackedOpens, &trackedClicks)
				if err != nil {
					t.Fatal(err)
				}
				if want.TotalEmailsSent > 0 {
					want.OpenRate = models.Rate(trackedOpens, want.OpenTrackedSent)
					want.ClickRate = models.Rate(trackedClicks, want.ClickTrackedSent)
					want.ReplyRate = float64(want.TotalReplies) / float64(want.TotalEmailsSent) * 100
					want.BounceRate = float64(want.TotalBounces) / float64(want.TotalEmailsSent) * 100
				}
				got, xerr := repo.GetDashboardOverallStats(ctx, sf.org, window[0], window[1], scope, mailboxes)
				if xerr != nil || got == nil || !reflect.DeepEqual(*got, want) {
					t.Fatalf("window=%v scope=%+v mailboxes=%v: got=%+v want=%+v err=%v", window, scope, mailboxes, got, want, xerr)
				}
			}
		}
	}
	exec(`WITH added AS (
		INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields,updated_at,created_at)
		SELECT gen_random_uuid(),$1,$2,g::text || '@history.test','','','','','{}',NOW(),NOW() FROM generate_series(1,20000) g RETURNING id)
		INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,sent_at)
		SELECT $3,id,$4,$5::timestamptz-interval '90 days' FROM added`, sf.owner, sf.org, sf.campaign, sequence, from)
	exec(`ANALYZE campaign_contact_progress`)
	conn, err := pgx.ConnectConfig(ctx, f.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `SET plan_cache_mode=force_generic_plan`); err != nil {
		t.Fatal(err)
	}
	before := legacyDashboardOverallSQL(nil, nil)
	after := before + ` AND ccp.sent_at >= $2 AND ccp.sent_at <= $3`
	args := fmt.Sprintf("'%s','%s'::timestamptz,'%s'::timestamptz", sf.org, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
	beforeRows, beforeMS, _ := taskPressureExplain(t, conn, before, args, "campaign_contact_progress")
	afterRows, afterMS, plan := taskPressureExplain(t, conn, after, args, "campaign_contact_progress")
	t.Logf("synthetic 20000 historical progress rows: generic plan before %.3fms / %.0f progress rows, after %.3fms / %.0f progress rows", beforeMS, beforeRows, afterMS, afterRows)
	if afterRows >= beforeRows || !strings.Contains(plan, "sent_at >=") {
		t.Fatalf("expected bounded progress scan: before=%v after=%v plan=%s", beforeRows, afterRows, plan)
	}
}
