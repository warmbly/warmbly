// Package notification owns per-user notification preferences + the in-app feed.
// Notify() is the gated ingress: it checks the user's preference for a category
// and, when enabled, persists a feed row and pushes a realtime event. It is
// best-effort and must never fail the caller's hot path (it runs inside inbox
// ingest), so all errors are swallowed.
package notification

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// EmailSender delivers a notification to a user's account email. Satisfied by
// notify.EmailNotificationService.
type EmailSender interface {
	Send(ctx context.Context, to, cc, bcc []string, subject, message string) error
}

// SlackNotice is one notification as Slack renders it.
type SlackNotice struct {
	Category          models.NotificationCategory
	Title, Body, Link string
	// Meta carries ids a card can act on, such as "unibox_email_id".
	Meta map[string]any
}

// SlackNotifier delivers to Slack. Satisfied by slackapp.Notifier.
type SlackNotifier interface {
	// NotifyOrg posts to the workspace's routed channel.
	NotifyOrg(ctx context.Context, orgID uuid.UUID, n SlackNotice) error
	// NotifyMember DMs one member; a no-op unless they linked Slack with DMs on.
	NotifyMember(ctx context.Context, orgID, userID uuid.UUID, n SlackNotice) error
}

// UserLookup resolves a user's email + name for email delivery. Satisfied by
// the user repository.
type UserLookup interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
}

// MemberLookup enumerates an org's members so NotifyOrg can resolve a
// permission-targeted audience. Satisfied by the organization repository.
type MemberLookup interface {
	GetMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationMember, error)
}

type Service interface {
	GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, *errx.Error)
	UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) *errx.Error
	List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, *errx.Error)
	UnreadCount(ctx context.Context, userID uuid.UUID) (int, *errx.Error)
	MarkRead(ctx context.Context, userID, notifID uuid.UUID) *errx.Error
	MarkAllRead(ctx context.Context, userID uuid.UUID) *errx.Error

	// Notify is the gated ingress — best-effort, never errors out the caller.
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)

	// NotifyAboutMessage is Notify for one unibox message: the row is removed
	// with the message and read when the message is read.
	NotifyAboutMessage(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, uniboxEmailID uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)

	// NotifyOrg raises the same notification for every accepted org member
	// holding perm (never the whole org blindly), excluding exclude when set.
	// Rows share groupKey, so the email flush coalesces the event into one
	// message with every recipient in To; Slack fires at most once. Each
	// member's own preferences still gate their channels. Best-effort.
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)
	// NotifyOrgAboutMessage is NotifyOrg for one unibox message.
	NotifyOrgAboutMessage(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, uniboxEmailID uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)

	// WireDelivery attaches the email + Slack + user/member-lookup
	// dependencies (wired post-construction in both mains) and starts the
	// email digest flush loop when the email channel is deliverable. Any
	// dependency may be nil — the matching channel is then skipped.
	WireDelivery(email EmailSender, slack SlackNotifier, users UserLookup, members MemberLookup)

	// WirePush attaches APNs + device-token storage + the Redis digest window
	// and starts the digest loop. Any nil dependency leaves push disabled.
	WirePush(sender PushSender, tokens repository.DeviceTokenRepository, rdb *redis.Client)

	// RegisterDevice / UnregisterDevice manage the caller's APNs tokens.
	RegisterDevice(ctx context.Context, userID uuid.UUID, platform, token, environment string) error
	UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error
}

type service struct {
	repo      repository.NotificationRepository
	publisher *pubsub.StreamingPublisher
	email     EmailSender
	slack     SlackNotifier
	users     UserLookup
	members   MemberLookup

	push         PushSender
	deviceTokens repository.DeviceTokenRepository
	pushRedis    *redis.Client
}

func (s *service) WireDelivery(email EmailSender, slack SlackNotifier, users UserLookup, members MemberLookup) {
	s.email = email
	s.slack = slack
	s.users = users
	s.members = members
	if s.email != nil && s.users != nil {
		go s.emailFlushLoop()
	}
}

func NewService(repo repository.NotificationRepository, publisher *pubsub.StreamingPublisher) Service {
	return &service{repo: repo, publisher: publisher}
}

func (s *service) GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, *errx.Error) {
	p, err := s.repo.GetPreferences(ctx, userID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return p, nil
}

func (s *service) UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) *errx.Error {
	if err := s.repo.UpdatePreferences(ctx, userID, prefs); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, *errx.Error) {
	out, err := s.repo.List(ctx, userID, limit, unreadOnly)
	if err != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

func (s *service) UnreadCount(ctx context.Context, userID uuid.UUID) (int, *errx.Error) {
	c, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return 0, errx.InternalError()
	}
	return c, nil
}

func (s *service) MarkRead(ctx context.Context, userID, notifID uuid.UUID) *errx.Error {
	if err := s.repo.MarkRead(ctx, userID, notifID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) MarkAllRead(ctx context.Context, userID uuid.UUID) *errx.Error {
	if err := s.repo.MarkAllRead(ctx, userID); err != nil {
		return errx.InternalError()
	}
	return nil
}

// Notify checks the user's preference for category and fans the enabled
// channels. Silent on any miss/error.
func (s *service) Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s == nil {
		return
	}
	s.notifyOne(ctx, userID, orgID, nil, category, title, body, link, meta, "", false)
}

func (s *service) NotifyAboutMessage(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, uniboxEmailID uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s == nil || uniboxEmailID == uuid.Nil {
		return
	}
	s.notifyOne(ctx, userID, orgID, &uniboxEmailID, category, title, body, link, meta, "", false)
}

// NotifyOrg resolves the permission-targeted audience and raises the
// notification for each member. Slack posts to one org workspace, so it fires
// for the first member whose prefs allow it and stays suppressed for the rest.
func (s *service) NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string) {
	s.notifyMembers(ctx, orgID, perm, exclude, nil, category, title, body, link, meta, groupKey)
}

// NotifyOrgAboutMessage is NotifyOrg for one unibox message, so each member's
// notification leaves with the message and is read with it.
func (s *service) NotifyOrgAboutMessage(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, uniboxEmailID uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string) {
	if uniboxEmailID == uuid.Nil {
		return
	}
	s.notifyMembers(ctx, orgID, perm, uuid.Nil, &uniboxEmailID, category, title, body, link, meta, groupKey)
}

func (s *service) notifyMembers(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, uniboxEmailID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string) {
	if s == nil || s.members == nil || orgID == uuid.Nil {
		return
	}
	members, err := s.members.GetMembers(ctx, orgID)
	if err != nil {
		return
	}
	org := orgID
	slackFired := false
	for _, m := range members {
		if m.AcceptedAt == nil || m.UserID == uuid.Nil || m.UserID == exclude {
			continue
		}
		if perm != 0 && !m.Permissions.HasPermission(perm) {
			continue
		}
		fired := s.notifyOne(ctx, m.UserID, &org, uniboxEmailID, category, title, body, link, meta, groupKey, slackFired)
		slackFired = slackFired || fired
	}
}

// notifyOne is the per-user ingress behind Notify/NotifyOrg. The feed row is
// the delivery record for both the in-app and email channels: email-channel
// rows queue as pending with a due time from the user's digest cadence, and
// the flush loop bundles them later (see email.go). Returns whether the Slack
// channel fired, so org fan-outs post to the shared workspace only once.
func (s *service) notifyOne(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, uniboxEmailID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string, suppressSlack bool) bool {
	fired, _ := s.notifyOneWithError(ctx, userID, orgID, uniboxEmailID, category, title, body, link, meta, groupKey, suppressSlack)
	return fired
}

func (s *service) NotifyDurable(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string) error {
	_, err := s.notifyOneWithError(ctx, userID, orgID, nil, category, title, body, link, meta, groupKey, false)
	return err
}

func (s *service) notifyOneWithError(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, uniboxEmailID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string, suppressSlack bool) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	prefs, err := s.repo.GetPreferences(ctx, userID)
	if err != nil || prefs == nil {
		return false, err
	}
	cat := prefs.CategoryPref(category)
	if !cat.Enabled {
		return false, nil
	}
	if !s.canNotifyMessage(ctx, category, uniboxEmailID) {
		return false, nil
	}

	emailOn := cat.Channels.Email && s.email != nil && s.users != nil
	if cat.Channels.InApp || emailOn {
		n := &models.Notification{
			UserID:         userID,
			OrganizationID: orgID,
			Category:       category,
			Title:          title,
			Body:           body,
			Link:           link,
			Metadata:       meta,
			UniboxEmailID:  uniboxEmailID,
			GroupKey:       groupKey,
			// In-app off but email on: keep the row as the email record
			// without ringing the bell.
			PreRead: !cat.Channels.InApp,
		}
		if emailOn {
			due := time.Now().Add(emailHold(prefs.EmailDigestMinutes, category))
			n.EmailState = "pending"
			n.EmailDueAt = &due
		}
		created, cerr := s.repo.Create(ctx, n)
		if errors.Is(cerr, repository.ErrNotificationMessageGone) || errors.Is(cerr, repository.ErrNotificationMessageAutomated) {
			return false, nil
		}
		if cerr != nil {
			return false, cerr
		}
		if cerr == nil && created != nil && created.MessageSeen {
			return false, nil
		}
		if cerr == nil && created != nil && cat.Channels.InApp && s.publisher != nil {
			s.publisher.PublishNotificationCreated(ctx, userID.String(), created.ID.String(), string(category), title, link)
		}
	}

	// Slack: the org channel once per notification, plus this member's DM
	// (detached, best-effort).
	slackFired := false
	if cat.Channels.Slack && s.slack != nil && orgID != nil {
		org, postOrg := *orgID, !suppressSlack
		slackFired = postOrg
		notice := slackNotice(category, title, body, link, meta, uniboxEmailID)
		go func(parent context.Context) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 8*time.Second)
			defer cancel()
			if !s.canNotifyMessage(ctx, category, uniboxEmailID) {
				return
			}
			if postOrg {
				if err := s.slack.NotifyOrg(ctx, org, notice); err != nil {
					log.Printf("notification: slack channel post failed (org=%s category=%s): %v", org, category, err)
				}
			}
			if err := s.slack.NotifyMember(ctx, org, userID, notice); err != nil {
				log.Printf("notification: slack dm failed (org=%s category=%s): %v", org, category, err)
			}
		}(ctx)
	}

	// Push: immediate on a quiet window, digest-batched inside one (detached).
	if cat.Channels.Push && s.push != nil && s.deviceTokens != nil && s.pushRedis != nil {
		go s.deliverPush(userID, category, pendingPush{Title: title, Body: body, Link: link, MessageID: uniboxEmailID})
	}
	return slackFired, nil
}

func (s *service) canNotifyMessage(ctx context.Context, category models.NotificationCategory, messageID *uuid.UUID) bool {
	allowed, err := s.messageEligibility(ctx, category, messageID)
	return err == nil && allowed
}

func (s *service) messageEligibility(ctx context.Context, category models.NotificationCategory, messageID *uuid.UUID) (bool, error) {
	if messageID == nil || (category != models.NotifInboundReply && category != models.NotifInboundOOO) {
		return true, nil
	}
	return s.repo.CanNotifyAboutMessage(ctx, *messageID, category)
}

// slackNotice copies meta so the detached delivery never shares the caller's map.
func slackNotice(category models.NotificationCategory, title, body, link string, meta map[string]any, uniboxEmailID *uuid.UUID) SlackNotice {
	m := make(map[string]any, len(meta)+1)
	for k, v := range meta {
		m[k] = v
	}
	if uniboxEmailID != nil && *uniboxEmailID != uuid.Nil {
		m["unibox_email_id"] = uniboxEmailID.String()
	}
	return SlackNotice{Category: category, Title: title, Body: body, Link: link, Meta: m}
}
