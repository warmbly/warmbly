package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type sendGateReader struct {
	repository.TaskRepository
	calls     int
	admission *repository.SendAdmission
	err       error
}

func (r *sendGateReader) GetSendAdmission(context.Context, uuid.UUID, uuid.UUID, models.InboxProvider, time.Time) (*repository.SendAdmission, error) {
	r.calls++
	return r.admission, r.err
}

func TestCampaignSendGateUsesDurableAdmissionOncePerPass(t *testing.T) {
	org := uuid.New()
	acct := models.Email{ID: uuid.New(), OrganizationID: &org, Provider: "smtp_imap"}
	retry := time.Now().Add(time.Hour)
	brief := time.Now().Add(10 * time.Minute)
	for _, tc := range []struct {
		name      string
		admission *repository.SendAdmission
		err       error
		want      string
		paced     bool
	}{
		{"unknown hold", &repository.SendAdmission{RecoveryHold: true, HoldReason: "unknown"}, nil, gateRecovery, false},
		{"permanent hold", &repository.SendAdmission{RecoveryHold: true, HoldReason: "permanent"}, nil, gateRecovery, false},
		{"domain auth hold", &repository.SendAdmission{RecoveryHold: true, HoldReason: "domain_auth"}, nil, gateRecovery, false},
		{"long cooldown", &repository.SendAdmission{RetryAt: &retry, PauseReason: "daily_limit"}, nil, gateCooldown, false},
		{"short cooldown", &repository.SendAdmission{RetryAt: &brief, PauseReason: "rate_limited"}, nil, gateCooldown, true},
		{"expired cooldown", &repository.SendAdmission{Allowed: true}, nil, "", false},
		{"inactive", &repository.SendAdmission{}, nil, gateAdmission, false},
		{"unavailable", nil, errors.New("authority unavailable"), gateAdmission, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &sendGateReader{admission: tc.admission, err: tc.err}
			s := &schedulerService{taskRepo: r}
			p := &campaignPass{}
			gate := s.sendGate(t.Context(), p, acct)
			if gate.reason != tc.want {
				t.Fatalf("gate=%+v want %s", gate, tc.want)
			}
			if gate.paced != tc.paced {
				t.Fatalf("paced=%t want %t: a short pause keeps the lead on its mailbox, a long one moves it", gate.paced, tc.paced)
			}
			if tc.want == gateRecovery || tc.want == gateCooldown {
				if _, remaining := s.gateFor(t.Context(), p, acct, 50); remaining != 0 {
					t.Fatal("closed mailbox contributes capacity")
				}
			}
			s.sendGate(t.Context(), p, acct)
			if r.calls != 1 {
				t.Fatal("routing and placement did not share admission snapshot")
			}
		})
	}
}
