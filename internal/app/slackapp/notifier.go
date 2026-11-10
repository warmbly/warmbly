package slackapp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/notification"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Notifier delivers notification cards to Slack. It needs no assistant
// wiring, so the consumer can build it on its own.
type Notifier struct {
	integ  Integrations
	repo   repository.SlackRepository
	client *Client
}

var _ notification.SlackNotifier = (*Notifier)(nil)

func NewNotifier(integ Integrations, repo repository.SlackRepository) *Notifier {
	return &Notifier{integ: integ, repo: repo, client: NewClient()}
}

// draftReplyValue is the "Draft a reply" button payload.
type draftReplyValue struct {
	EmailID  string `json:"e,omitempty"`
	ThreadID string `json:"t,omitempty"`
}

// NotifyOrg posts the card to the category's routed channel, else the default.
func (n *Notifier) NotifyOrg(ctx context.Context, orgID uuid.UUID, notice notification.SlackNotice) error {
	if n == nil || n.integ == nil {
		return nil
	}
	conn, err := n.integ.SlackConnection(ctx, orgID)
	if err != nil || conn == nil {
		return err
	}
	channel := routeChannel(settingsFrom(conn), notice.Category)
	if channel == "" {
		channel = n.integ.SlackDefaultChannel(ctx, orgID, conn)
	}
	if channel == "" {
		return nil
	}
	token, err := n.integ.SlackBotToken(ctx, orgID, conn.ID)
	if err != nil {
		return err
	}
	msg := noticeMessage(notice)
	msg.Channel = channel
	_, err = n.client.PostMessage(ctx, token, msg)
	return err
}

// NotifyMember DMs a member who linked Slack and turned DM notifications on.
func (n *Notifier) NotifyMember(ctx context.Context, orgID, userID uuid.UUID, notice notification.SlackNotice) error {
	if n == nil || n.repo == nil || n.integ == nil {
		return nil
	}
	link, err := n.repo.GetLinkForUser(ctx, orgID, userID)
	if err != nil || link == nil || !link.DMNotifications {
		return err
	}
	token, err := n.integ.SlackBotToken(ctx, orgID, link.ConnectionID)
	if err != nil {
		return err
	}
	dm, err := n.client.OpenDM(ctx, token, link.SlackUserID)
	if err != nil {
		return err
	}
	msg := noticeMessage(notice)
	msg.Channel = dm
	_, err = n.client.PostMessage(ctx, token, msg)
	return err
}

// routeChannel picks the category's channel, falling back to the default.
func routeChannel(st models.SlackSettings, cat models.NotificationCategory) string {
	if ch := strings.TrimSpace(st.Routes[cat]); ch != "" {
		return ch
	}
	return strings.TrimSpace(st.Channel)
}

// noticeMessage renders a notification as a card with its actions.
func noticeMessage(n notification.SlackNotice) Message {
	title := strings.TrimSpace(n.Title)
	text := "*" + escapeMrkdwn(title) + "*"
	if body := strings.TrimSpace(n.Body); body != "" {
		text += "\n" + escapeMrkdwn(body)
	}
	var draft Block
	if n.Category.IsHumanReply() {
		v := draftReplyValue{EmailID: metaString(n.Meta, "unibox_email_id"), ThreadID: metaString(n.Meta, "thread_id")}
		if v.EmailID != "" || v.ThreadID != "" {
			raw, _ := json.Marshal(v)
			draft = actionButton("Draft a reply", ActionDraftReply, string(raw), "")
		}
	}
	return Message{
		Text: truncateRunes(title, maxFallbackText),
		Blocks: blocks(
			sectionBlock(text),
			buttonsBlock(urlButton("Open in Warmbly", appURL(n.Link)), draft),
		),
	}
}

func metaString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
