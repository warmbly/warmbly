package tasks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/repository"
)

type testTaskPublisher struct {
	events.Publisher
	t      *testing.T
	f      *sendFixture
	params *events.SendEmailParams
}

func (p *testTaskPublisher) PublishSendEmail(ctx context.Context, _ uuid.UUID, params *events.SendEmailParams) error {
	p.t.Helper()
	var task, nonce, message string
	if err := p.f.pool.QueryRow(ctx, `SELECT task_type,send_executor_nonce::text,message_id FROM tasks WHERE id=$1`, params.TaskID).Scan(&task, &nonce, &message); err != nil || task != "email" || nonce != params.DispatchNonce || message != params.MessageID || message == "" {
		p.t.Fatal("published without real admitted task", task, nonce, message, err)
	}
	p.params = params
	return nil
}

func TestLiveCampaignTestSendCreatesTaskBeforeSharedAdmissionAndKeepsTrackingDisabled(t *testing.T) {
	f := newSendFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, []any{worker}},
		{`INSERT INTO workers(id)VALUES($1)`, []any{worker}},
		{`UPDATE email_accounts SET worker_id=$1,shared_daily_limit=1 WHERE id=$2`, []any{worker, f.mailbox}},
	} {
		if _, err := f.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	pub := &testTaskPublisher{t: t, f: f}
	sender := NewEmailSender(f.svc.emailRepo, pub).(*emailSender)
	sender.WireSendAdmission(f.svc.taskRepo.(repository.OutboundAdmissionRepository))
	f.svc.emailSender = sender
	campaign, err := f.svc.campaignRepo.GetByID(ctx, f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := f.svc.campaignRepo.GetSequenceByID(ctx, f.step)
	if err != nil {
		t.Fatal(err)
	}
	if xerr := f.svc.SendTestEmail(ctx, f.org, f.mailbox, f.emailAAddr, campaign, sequence, nil); xerr != nil {
		t.Fatal(xerr)
	}
	if pub.params == nil || pub.params.Subject != "[TEST] Hi" || pub.params.TrackingInfo != nil || pub.params.UnsubscribeURL != "" {
		t.Fatal("test labeling/tracking changed", pub.params)
	}
	pub.params = nil
	if xerr := f.svc.SendTestEmail(ctx, f.org, f.mailbox, f.emailAAddr, campaign, sequence, nil); xerr == nil || pub.params != nil {
		t.Fatal("test bypassed mandatory admission", xerr)
	}
	var failures int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM task_failures f JOIN tasks t ON t.id=f.task_id WHERE t.email_account_id=$1 AND t.status='failed'`, f.mailbox).Scan(&failures); err != nil || failures != 1 {
		t.Fatal("rejected test disappeared without failure", failures, err)
	}
}
