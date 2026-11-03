package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestMailboxLoadingRequiresPreparationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    string
		failure *errx.SendFailure
		loading bool
	}{
		{name: "missing protocol evidence"},
		{name: "preparation", failure: &errx.SendFailure{Protocol: "internal", Stage: "prepare", Disposition: errx.SendRetry}, loading: true},
		{name: "provider retry", failure: &errx.SendFailure{Protocol: "smtp", Stage: "connect", Disposition: errx.SendRetry}},
		{name: "ambiguous", failure: &errx.SendFailure{Protocol: "internal", Stage: "prepare", Disposition: errx.SendAmbiguous}},
		{name: "authentication", code: string(errx.MailErrorCodeInvalidCredentials)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := sendFailureReason(models.SendEmailResult{Error: &models.EmailSendError{Code: tc.code, Message: models.MailboxNotLoadedPrefix, Failure: tc.failure}})
			if (code == models.MailboxLoadingCode) != tc.loading {
				t.Fatalf("code=%s, loading=%t", code, tc.loading)
			}
		})
	}
}

func TestLiveCampaignMailboxLoadingDefersWithoutSpendingAttempts(t *testing.T) {
	s, handle := liveWarmupService(t)
	f := newSendResultFixture(t, handle)
	s.TaskRepo = repository.NewTaskRepository(handle.Pool)
	s.CampaignRepo = repository.NewCampaignRepostory(handle)
	s.CampaignProgressRepo = repository.NewCampaignProgressRepository(handle.Pool)
	s.CampaignLogRepo = repository.NewCampaignLogRepository(handle)
	s.ContactRepo = repository.NewContactRepostory(handle)
	ctx := context.Background()
	var historicalTask string
	for i := 0; i < 6; i++ {
		task := f.stampSend(t, s)
		result := models.SendEmailResult{TaskID: task, SentAt: time.Now(), Error: &models.EmailSendError{
			Code: string(errx.MailErrorCodeServerUnreachable), Message: models.MailboxNotLoadedPrefix,
			Failure: &errx.SendFailure{Protocol: "internal", Stage: "prepare", Disposition: errx.SendRetry, ObservedAt: time.Now()},
		}}
		for duplicate := 0; duplicate < 2; duplicate++ {
			if err := s.HandleEmailFailed(ctx, result); err != nil {
				t.Fatal(err)
			}
		}
		if i == 0 {
			historicalTask = task.String()
			if _, err := handle.Exec(ctx, `UPDATE campaign_logs SET event_type='email_failed', metadata=(metadata-'mailbox_loading_retry') || '{"level":"error","code":"SERVER_UNREACHABLE"}'::jsonb WHERE campaign_id=$1 AND metadata->>'task_id'=$2`, f.campaign, historicalTask); err != nil {
				t.Fatal(err)
			}
		}
		var attempts, dailySends int
		var sent, dispatched *time.Time
		if err := handle.QueryRow(ctx, `SELECT send_attempts,sent_at,dispatched_at FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts, &sent, &dispatched); err != nil {
			t.Fatal(err)
		}
		if err := handle.QueryRow(ctx, `SELECT COALESCE(SUM(emails_sent),0) FROM campaign_daily_sends WHERE campaign_id=$1`, f.campaign).Scan(&dailySends); err != nil {
			t.Fatal(err)
		}
		if attempts != 0 || sent != nil || dispatched != nil || dailySends != 0 {
			t.Fatalf("unattempted send spent budget or stranded its reservation: attempts=%d daily=%d sent=%v dispatched=%v", attempts, dailySends, sent, dispatched)
		}
	}
	logs, err := s.CampaignLogRepo.GetLogs(ctx, f.campaign, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Data) != 6 {
		t.Fatalf("duplicate results changed the log count: %d", len(logs.Data))
	}
	for _, entry := range logs.Data {
		if entry.EventType != "email_deferred" || entry.Metadata["level"] != "info" || entry.Metadata["will_retry"] != true {
			t.Fatalf("reconnect became a customer alert: %+v", entry)
		}
		if entry.Metadata["code"] == models.MailboxLoadingCode && entry.Metadata["mailbox_loading_retry"] != true {
			t.Fatalf("confirmed retry marker missing: %+v", entry)
		}
	}
	if _, err := handle.Exec(ctx, `UPDATE tasks SET send_result_evidence=jsonb_set(send_result_evidence,'{protocol}','"smtp"') WHERE id=$1::uuid`, historicalTask); err != nil {
		t.Fatal(err)
	}
	logs, err = s.CampaignLogRepo.GetLogs(ctx, f.campaign, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	var providerRetained bool
	for _, entry := range logs.Data {
		if entry.Metadata["task_id"] == historicalTask {
			providerRetained = entry.EventType == "email_failed" && entry.Metadata["level"] == "error"
		}
	}
	if !providerRetained {
		t.Fatal("provider evidence was reclassified from log text alone")
	}
	other := newSendResultFixture(t, handle)
	if err := s.CampaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{CampaignID: other.campaign, EventType: "email_failed", Message: "unconfirmed reconnect", Metadata: map[string]interface{}{
		"level": "error", "code": string(errx.MailErrorCodeServerUnreachable), "error": models.MailboxNotLoadedPrefix, "will_retry": true, "task_id": historicalTask,
	}}); err != nil {
		t.Fatal(err)
	}
	otherLogs, err := s.CampaignLogRepo.GetLogs(ctx, other.campaign, 100, nil)
	if err != nil || len(otherLogs.Data) != 1 || otherLogs.Data[0].Metadata["level"] != "error" {
		t.Fatalf("another campaign's task must not authorize reclassification: %+v err=%v", otherLogs, err)
	}
	task := f.stampSend(t, s)
	if err := s.HandleEmailFailed(ctx, models.SendEmailResult{TaskID: task, Error: &models.EmailSendError{Code: string(errx.MailErrorCodeServerUnreachable), Message: "provider connection failed", Failure: &errx.SendFailure{Protocol: "smtp", Stage: "connect", Disposition: errx.SendRetry, ObservedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := handle.QueryRow(ctx, `SELECT send_attempts FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("provider failure must still spend an attempt: attempts=%d err=%v", attempts, err)
	}
	logs, err = s.CampaignLogRepo.GetLogs(ctx, f.campaign, 100, nil)
	if err != nil || logs.Data[0].Metadata["level"] != "error" {
		t.Fatalf("provider failure must stay actionable: %+v err=%v", logs, err)
	}
}

func TestServerPacingNeedsAnSMTPRefusalBeforeTheRecipient(t *testing.T) {
	unreachable := string(errx.MailErrorCodeServerUnreachable)
	for _, tc := range []struct {
		name    string
		code    string
		failure *errx.SendFailure
		pacing  bool
	}{
		{name: "held on the worker", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Stage: "pacing", Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}, pacing: true},
		{name: "refused at sign-in", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Stage: "auth", Status: 454, Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}, pacing: true},
		{name: "plain connection failure", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Stage: "starttls", Disposition: errx.SendRetry}},
		{name: "other protocol", code: unreachable, failure: &errx.SendFailure{Protocol: "graph", Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}},
		{name: "not a retry", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Cause: errx.SendCauseServerPacing, Disposition: errx.SendPermanent}},
		{name: "other code", code: string(errx.MailErrorCodeInvalidCredentials), failure: &errx.SendFailure{Protocol: "smtp", Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := sendFailureReason(models.SendEmailResult{Error: &models.EmailSendError{Code: tc.code, Message: "refused", Failure: tc.failure}})
			if (code == models.ServerPacingCode) != tc.pacing {
				t.Fatalf("code=%s, pacing=%t", code, tc.pacing)
			}
		})
	}
}
