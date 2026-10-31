package analytics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestSendSafetyHoldCannotAppearHealthyOrPromiseAutomaticSendRetries(t *testing.T) {
	for _, reason := range []string{"unknown", "authentication", "conflict", "permanent"} {
		t.Run(reason, func(t *testing.T) {
			id := uuid.New()
			for _, withError := range []bool{false, true} {
				var errs []models.AccountError
				if withError {
					errs = []models.AccountError{
						{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Operation: models.MailTransportSend},
						{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Operation: models.MailTransportSync},
						{ErrorCode: string(errx.MailErrorCodeConnectionLost), Operation: models.MailTransportUnknown},
					}
				}
				s := &analyticsService{}
				status := s.assembleAccountStatus(context.Background(), &models.Email{ID: id, Status: "active", Provider: "smtp_imap"},
					&models.AccountDailyUsage{}, errs, nil, false, nil, false,
					&statusReads{sendHolds: map[uuid.UUID]repository.SendRecoveryHold{id: {Reason: reason}}})
				if status.Health.Status != "error" || status.Health.Score > 50 || len(status.Health.Issues) == 0 {
					t.Fatalf("held mailbox cannot look healthy, even without an error row: %+v", status.Health)
				}
				if !withError {
					continue
				}
				if !strings.Contains(*status.Errors[1].ActionRequired, "retries automatically") {
					t.Fatal("send hold must not stop independent sync recovery")
				}
				for _, i := range []int{0, 2} {
					action := *status.Errors[i].ActionRequired
					if strings.Contains(action, "retries automatically") || !strings.Contains(action, "paused") {
						t.Fatalf("held sending must not promise retries: %s", action)
					}
					if reason == "unknown" && (!strings.Contains(action, "sent twice") || !strings.Contains(action, "on its own") || strings.Contains(action, "Provider confirmation")) {
						t.Fatalf("unknown outcome recovers automatically without a resend: %s", action)
					}
				}
			}
		})
	}
}

func TestUnknownWarmupHoldExplainsAutomaticRetirementWithoutPromisingResend(t *testing.T) {
	id := uuid.New()
	s := &analyticsService{}
	status := s.assembleAccountStatus(t.Context(), &models.Email{ID: id, Status: "active", Provider: "smtp_imap"},
		&models.AccountDailyUsage{}, []models.AccountError{{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Operation: models.MailTransportSend}},
		nil, false, nil, false, &statusReads{sendHolds: map[uuid.UUID]repository.SendRecoveryHold{id: {Reason: "unknown", Warmup: true}}})
	if status.Health.Status != "error" || status.Health.Score > 50 {
		t.Fatalf("recovery has not completed: %+v", status.Health)
	}
	for _, message := range []string{status.Health.Issues[len(status.Health.Issues)-1], *status.Errors[0].ActionRequired} {
		if !strings.Contains(message, "automatically") || !strings.Contains(message, "new sends") || !strings.Contains(message, "never retried") || strings.Contains(message, "Provider confirmation is required") {
			t.Fatalf("incorrect warmup recovery guidance: %s", message)
		}
	}
}

func TestUnknownCampaignHoldExplainsAutomaticIsolation(t *testing.T) {
	message := sendRecoveryHoldMessage(repository.SendRecoveryHold{Reason: "unknown", Campaign: true})
	for _, required := range []string{"automatically", "other contacts", "never retried", "No action is needed"} {
		if !strings.Contains(message, required) {
			t.Fatalf("campaign recovery guidance missing %q: %s", required, message)
		}
	}
}

type statusSendHoldReader struct {
	repository.EmailRepository
	calls int
	ids   []uuid.UUID
	holds map[uuid.UUID]repository.SendRecoveryHold
	err   error
}

func (r *statusSendHoldReader) GetSendRecoveryHolds(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]repository.SendRecoveryHold, error) {
	r.calls++
	r.ids = ids
	return r.holds, r.err
}

func TestStatusReadsLoadSendHoldsOnceEvenWithoutWarmup(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	r := &statusSendHoldReader{holds: map[uuid.UUID]repository.SendRecoveryHold{ids[1]: {Reason: "unknown"}}}
	s := &analyticsService{emailRepo: r}
	reads := s.loadStatusReads(t.Context(), []models.Email{{ID: ids[0]}, {ID: ids[1]}}, nil)
	if r.calls != 1 || len(r.ids) != 2 || r.ids[0] != ids[0] || r.ids[1] != ids[1] || reads.sendHolds[ids[1]].Reason != "unknown" {
		t.Fatalf("status must batch holds for the authorized mailboxes: calls=%d ids=%v reads=%+v", r.calls, r.ids, reads)
	}
	r.err = errors.New("hold query unavailable")
	reads = s.loadStatusReads(t.Context(), []models.Email{{ID: ids[0]}}, nil)
	status := s.assembleAccountStatus(t.Context(), &models.Email{ID: ids[0], Status: "active"}, &models.AccountDailyUsage{}, nil, nil, false, nil, false, reads)
	if status.Health.Status == "healthy" || !strings.Contains(strings.Join(status.Health.Issues, " "), "unavailable") {
		t.Fatalf("failed hold read must not claim healthy: %+v", status.Health)
	}
}

func TestAccountErrorsGroupIdenticalReportsWithoutHidingDistinctFailures(t *testing.T) {
	now := time.Now().UTC()
	row := repository.EmailAccountError{
		ID: uuid.New(), ErrorCode: string(errx.MailErrorCodeServerUnreachable),
		Severity: "WARNING", Title: "Connection Error", Message: "Connection failed",
		Operation: models.MailTransportSync, CreatedAt: now,
	}
	duplicate := row
	duplicate.ID = uuid.New()
	duplicate.CreatedAt = now.Add(-time.Hour)
	duplicate.Severity = "CRITICAL"
	send := row
	send.ID = uuid.New()
	send.Operation = models.MailTransportSend
	different := row
	different.ID = uuid.New()
	different.Message = "Secure connection failed"
	unknown := row
	unknown.ID = uuid.New()
	unknown.Operation = ""

	errs := toAccountErrors([]repository.EmailAccountError{duplicate, row, send, different, unknown})
	if len(errs) != 4 || errs[0].ID != row.ID || !errs[0].CreatedAt.Equal(now) || errs[0].Severity != "CRITICAL" {
		t.Fatalf("identical reports should retain latest identity/time and strongest severity: %+v", errs)
	}
	if errs[1].Operation != models.MailTransportSend || errs[2].Message != different.Message || errs[3].Operation != models.MailTransportUnknown {
		t.Fatalf("distinct operation, detail and legacy unknown scope must remain separate: %+v", errs)
	}
	if got := toAccountErrors(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty errors must be a non-nil list: %+v", got)
	}
}

func TestAccountErrorsKeepWarningWhenNewestIdenticalReportIsInformational(t *testing.T) {
	row := repository.EmailAccountError{ID: uuid.New(), ErrorCode: "CODE", Message: "same", Severity: "WARNING", CreatedAt: time.Now()}
	newer := row
	newer.CreatedAt = row.CreatedAt.Add(time.Minute)
	newer.Severity = "INFORMATIONAL"
	for _, rows := range [][]repository.EmailAccountError{{row, newer}, {newer, row}} {
		errs := toAccountErrors(rows)
		if len(errs) != 1 || errs[0].Severity != "WARNING" || !errs[0].CreatedAt.Equal(newer.CreatedAt) {
			t.Fatalf("active warning must not be lost: %+v", errs)
		}
	}
}

func TestAccountErrorsPreferStoredUserMessage(t *testing.T) {
	message := "Provider connection failed"
	errs := toAccountErrors([]repository.EmailAccountError{{Message: "technical detail", UserMessage: &message}})
	if errs[0].Message != message {
		t.Fatalf("display should honor stored user-facing message: %+v", errs)
	}
}

func TestAccountHealthCountsEachErrorCodeOnceAtStrongestSeverity(t *testing.T) {
	for _, provider := range []string{"gmail", "outlook", "smtp_imap"} {
		t.Run(provider, func(t *testing.T) {
			email := &models.Email{Status: "active", Provider: provider}
			errs := []models.AccountError{
				{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Severity: "WARNING", Title: "Connection Error", Operation: models.MailTransportUnknown},
				{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Severity: "WARNING", Title: "Connection Error", Operation: models.MailTransportSend},
				{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Severity: "WARNING", Title: "Connection Error", Operation: models.MailTransportSync},
			}
			health := calculateAccountHealth(email, errs)
			if health.Score != 90 || health.Status != "warning" || len(health.Issues) != 1 {
				t.Fatalf("one failure category must not become three deductions: %+v", health)
			}
			errs[2].Severity = "CRITICAL"
			errs = append(errs, models.AccountError{ErrorCode: string(errx.MailErrorCodeAuthenticationFailed), Severity: "CRITICAL", Title: "Authentication Required"})
			health = calculateAccountHealth(email, errs)
			if health.Score != 40 || health.Status != "error" || len(health.Issues) != 2 {
				t.Fatalf("strongest severity and independent sign-in safeguards must remain: %+v", health)
			}
			email.Status = "inactive"
			if health := calculateAccountHealth(email, errs); health.Score != 0 || health.Status != "error" {
				t.Fatalf("inactive account penalty must remain: %+v", health)
			}
		})
	}
}

func TestConnectionErrorGuidanceNamesProviderAndOperationWithoutInventingAnOutage(t *testing.T) {
	for provider, name := range map[string]string{"gmail": "Gmail", "outlook": "Microsoft Outlook", "smtp_imap": "Mail server"} {
		for _, operation := range []models.MailTransportOperation{models.MailTransportSend, models.MailTransportSync, models.MailTransportUnknown} {
			t.Run(provider+"/"+string(operation), func(t *testing.T) {
				errs := []models.AccountError{{ErrorCode: string(errx.MailErrorCodeServerUnreachable), Operation: operation, Message: "The last attempt failed", Severity: "WARNING"}}
				describeAccountErrors(&models.Email{Status: "active", Provider: provider}, errs)
				if !strings.HasPrefix(errs[0].Title, name+" ") || !strings.Contains(errs[0].Title, "failed") || errs[0].Message != "The last attempt failed" || errs[0].Severity != "WARNING" {
					t.Fatalf("provider/operation copy must not replace actual evidence or severity: %+v", errs)
				}
				if operation.Known() && !strings.Contains(errs[0].Title, map[models.MailTransportOperation]string{models.MailTransportSend: "sending", models.MailTransportSync: "sync"}[operation]) {
					t.Fatalf("known operation should be visible: %+v", errs)
				}
				if errs[0].ActionRequired == nil || !strings.Contains(*errs[0].ActionRequired, "not proof") || !strings.Contains(*errs[0].ActionRequired, "automatically") {
					t.Fatalf("must explain automatic retries and unverified recovery: %+v", errs)
				}
				describeAccountErrors(&models.Email{Status: "inactive", Provider: provider}, errs)
				if !strings.Contains(*errs[0].ActionRequired, "automatic retries are paused") {
					t.Fatalf("a switched-off mailbox cannot retry automatically: %+v", errs)
				}
			})
		}
	}
	for _, code := range []errx.MailErrorCode{errx.MailErrorCodeAuthenticationFailed, errx.MailErrorCodeGoogleAuth, errx.MailErrorCodeInvalidCredentials, errx.MailErrorCodeAuthUnsupported, errx.MailErrorCodeRateLimitExceeded, errx.MailErrorCodeQuotaExceeded, errx.MailErrorCodeAccountSuspended} {
		action := "Existing specific action"
		errs := []models.AccountError{{ErrorCode: string(code), Title: "Specific issue", ActionRequired: &action}}
		describeAccountErrors(&models.Email{Status: "active", Provider: "gmail"}, errs)
		if errs[0].Title != "Specific issue" || *errs[0].ActionRequired != action {
			t.Fatalf("do not relabel %s as a retryable connection issue: %+v", code, errs)
		}
	}
}
