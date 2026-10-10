package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type messageNotificationRepo struct {
	repository.NotificationRepository
	allowed         bool
	err             error
	checks, created int
	channels        models.ChannelPrefs
	skipped         []uuid.UUID
	retry           []uuid.UUID
}

func (r *messageNotificationRepo) CanNotifyAboutMessage(context.Context, uuid.UUID, models.NotificationCategory) (bool, error) {
	r.checks++
	return r.allowed, r.err
}

func (r *messageNotificationRepo) GetPreferences(context.Context, uuid.UUID) (*models.NotificationPreferences, error) {
	prefs := models.DefaultNotificationPreferences()
	prefs.InboundReply = models.CategoryPref{Enabled: true, Channels: r.channels}
	prefs.CampaignPaused = models.CategoryPref{Enabled: true, Channels: r.channels}
	prefs.SecuritySignIn = models.CategoryPref{Enabled: true, Channels: r.channels}
	return &prefs, nil
}

func (r *messageNotificationRepo) Create(_ context.Context, n *models.Notification) (*models.Notification, error) {
	r.created++
	return n, nil
}

func (r *messageNotificationRepo) SkipEmails(_ context.Context, ids []uuid.UUID) error {
	r.skipped = append(r.skipped, ids...)
	return nil
}

func (r *messageNotificationRepo) RequeueEmails(_ context.Context, ids []uuid.UUID, _ time.Duration, _ int) error {
	r.retry = append(r.retry, ids...)
	return nil
}

func TestEmailRechecksTheMessageAfterClaiming(t *testing.T) {
	id, notificationID, actionID := uuid.New(), uuid.New(), uuid.New()
	userID, orgID := uuid.New(), uuid.New()
	r := &messageNotificationRepo{}
	s := &service{repo: r, members: acceptedMember(userID)}
	kept := s.keepEligibleEmailMessages(context.Background(), []models.Notification{
		{ID: notificationID, UserID: userID, OrganizationID: &orgID, Category: models.NotifInboundReply, UniboxEmailID: &id},
		{ID: actionID, UserID: userID, OrganizationID: &orgID, Category: models.NotifInboxActionRequired, UniboxEmailID: &id},
	})
	if len(kept) != 1 || kept[0].ID != actionID || len(r.skipped) != 1 || r.skipped[0] != notificationID {
		t.Fatalf("kept=%v, skipped=%v", kept, r.skipped)
	}
	r.skipped = nil
	r.err = errors.New("database unavailable")
	kept = s.keepEligibleEmailMessages(context.Background(), []models.Notification{
		{ID: notificationID, UserID: userID, OrganizationID: &orgID, Category: models.NotifInboundReply, UniboxEmailID: &id},
	})
	if len(kept) != 0 || len(r.skipped) != 0 || len(r.retry) != 1 || r.retry[0] != notificationID {
		t.Fatalf("eligibility lookup failure did not retry: kept=%v skipped=%v retry=%v", kept, r.skipped, r.retry)
	}
}

func TestNotifyAboutMessageGatesEveryChannel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		allowed  bool
		err      error
		channels models.ChannelPrefs
	}{
		{"in app", false, nil, models.ChannelPrefs{InApp: true}},
		{"slack only", false, nil, models.ChannelPrefs{Slack: true}},
		{"push only", false, nil, models.ChannelPrefs{Push: true}},
		{"email only", false, nil, models.ChannelPrefs{Email: true}},
		{"lookup failed", false, errors.New("database unavailable"), models.ChannelPrefs{InApp: true}},
		{"human reply", true, nil, models.ChannelPrefs{InApp: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID, orgID := uuid.New(), uuid.New()
			r := &messageNotificationRepo{allowed: tc.allowed, err: tc.err, channels: tc.channels}
			s := &service{repo: r, members: acceptedMember(userID)}
			s.NotifyAboutMessage(context.Background(), userID, &orgID, uuid.New(), models.NotifInboundReply, "Reply", "", "", nil)
			if r.checks != 1 {
				t.Fatalf("eligibility checks = %d, want 1", r.checks)
			}
			if (r.created == 1) != tc.allowed {
				t.Fatalf("created = %d, allowed = %v", r.created, tc.allowed)
			}
		})
	}
}

func TestPendingPushRechecksTheMessageButKeepsActionAlerts(t *testing.T) {
	id := uuid.New()
	r := &messageNotificationRepo{}
	s := &service{repo: r}
	for _, category := range []models.NotificationCategory{models.NotifInboundReply, models.NotifInboundOOO} {
		if s.canPushMessage(context.Background(), category, pendingPush{MessageID: &id}) {
			t.Errorf("%s accepted an automated message", category)
		}
		if s.canPushMessage(context.Background(), category, pendingPush{Body: "Report Domain: example.test Submitter: seznam.cz Report-ID: abc"}) {
			t.Errorf("%s accepted a legacy report batch", category)
		}
	}
	if !s.canPushMessage(context.Background(), models.NotifInboxActionRequired, pendingPush{MessageID: &id}) {
		t.Fatal("action alert was suppressed")
	}
	r.allowed = true
	if !s.canPushMessage(context.Background(), models.NotifInboundReply, pendingPush{MessageID: &id}) {
		t.Fatal("genuine reply was suppressed")
	}
}
