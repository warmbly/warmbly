package poollink

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

type tokenStatusAnalytics struct {
	analytics.AnalyticsService
	status *models.EmailAccountStatus
	xerr   *errx.Error
}

func (a tokenStatusAnalytics) GetAccountStatus(context.Context, uuid.UUID, uuid.UUID) (*models.EmailAccountStatus, *errx.Error) {
	return a.status, a.xerr
}

type statusTokenEmails struct {
	email.EmailService
	calls int
}

func (s *statusTokenEmails) OAuthAccessToken(context.Context, uuid.UUID) (*oauth2.Token, *errx.Error) {
	s.calls++
	return &oauth2.Token{AccessToken: "fixture-token"}, nil
}

type statusTokenRepo struct{ adoptLinkRepo }

func (*statusTokenRepo) TouchMailboxToken(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func TestAccessTokenRequiresAvailableMailboxStanding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status *models.EmailAccountStatus
		xerr   *errx.Error
		want   *errx.Error
	}{
		{name: "unavailable", xerr: errx.InternalError(), want: errx.InternalError()},
		{name: "missing", want: errx.InternalError()},
		{name: "blocked", status: &models.EmailAccountStatus{WarmupHealth: &models.WarmupHealthInfo{State: "blocked"}}, want: ErrMailboxBlocked},
		{name: "available", status: &models.EmailAccountStatus{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			org := uuid.New()
			acc := &models.Email{ID: uuid.New(), OrganizationID: &org, Status: "active", Provider: "google"}
			repo := &statusTokenRepo{adoptLinkRepo{enrolled: &models.PoolLinkMailbox{EmailAccountID: acc.ID, Managed: true}}}
			emails := &statusTokenEmails{}
			s := &service{repo: repo, emails: &adoptAccounts{acc: acc}, emailSvc: emails, analytics: tokenStatusAnalytics{status: tc.status, xerr: tc.xerr}}
			got, xerr := s.AccessToken(t.Context(), &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org}, uuid.New())
			if tc.want != nil {
				if got != nil || xerr == nil || xerr.Code != tc.want.Code || xerr.Identifier != tc.want.Identifier || emails.calls != 0 {
					t.Fatalf("token=%v error=%v calls=%d", got, xerr, emails.calls)
				}
			} else if xerr != nil || got == nil || emails.calls != 1 {
				t.Fatalf("token=%v error=%v calls=%d", got, xerr, emails.calls)
			}
		})
	}
}
