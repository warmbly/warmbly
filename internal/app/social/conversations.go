package social

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	previewRunes = 140
	// pullEvery spaces out the conversation pulls a conversation list starts per workspace.
	pullEvery = time.Minute
)

// Conversation is a social conversation as /v1/social shows it.
type Conversation struct {
	models.SocialConversation
	LastMessagePreview string `json:"last_message_preview"`
}

// Message is one social message, its body opened with the workspace's key.
type Message struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	Channel        string     `json:"channel,omitempty"`
	Direction      string     `json:"direction"`
	Body           string     `json:"body"`
	SentAt         *time.Time `json:"sent_at"`
	Status         string     `json:"status"`
	Kind           string     `json:"kind"`
	SentBy         *uuid.UUID `json:"sent_by"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ConversationFilter narrows GET /v1/social/conversations.
type ConversationFilter struct {
	AccountID  *uuid.UUID
	Channel    string
	ContactID  *uuid.UUID
	UnreadOnly bool
	Cursor     string
	Limit      int
}

func (s *Service) seal(ctx context.Context, c *cipher.Cipher, plain string) string {
	if c == nil || plain == "" {
		return ""
	}
	out, err := c.Encrypt(ctx, plain)
	if err != nil {
		return ""
	}
	return out
}

func (s *Service) open(ctx context.Context, c *cipher.Cipher, sealed string) string {
	if c == nil || sealed == "" {
		return ""
	}
	out, err := c.Decrypt(ctx, sealed)
	if err != nil {
		return ""
	}
	return out
}

func (s *Service) cipherFor(ctx context.Context, orgID uuid.UUID) (*cipher.Cipher, error) {
	if s.Cipher == nil {
		return nil, nil
	}
	return s.Cipher.Cipher(ctx, orgID)
}

func preview(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(body) <= previewRunes {
		return body
	}
	return string([]rune(body)[:previewRunes-1]) + "…"
}

// storeConversation mirrors a GraphKit conversation and matches its participant to a contact.
func (s *Service) storeConversation(ctx context.Context, orgID uuid.UUID, c *graphkit.Conversation, ci *cipher.Cipher) (*models.SocialConversation, error) {
	acct, err := s.Repo.GetAccountByGraphKitID(ctx, orgID, c.AccountID)
	if err != nil || acct == nil {
		return nil, err
	}
	channel := c.Channel
	if !models.IsSocialChannel(channel) {
		channel = acct.Channel
	}
	row := &models.SocialConversation{OrganizationID: orgID, AccountID: acct.ID, Channel: channel, GraphKitID: c.ID, Participant: mustJSON(c.Participant),
		Kind: c.Kind, UnreadCount: c.UnreadCount, LastMessageAt: c.LastMessageAt}
	if row.Kind == "" {
		row.Kind = "chat"
	}
	if c.LastMessage != nil {
		row.LastMessagePreview = s.seal(ctx, ci, preview(c.LastMessage.Body))
		if row.LastMessageAt == nil {
			row.LastMessageAt = c.LastMessage.SentAt
		}
	}
	row.ContactID = s.participantContact(ctx, orgID, channel, c.Participant)
	return s.Repo.UpsertConversation(ctx, row)
}

// participantContact is the contact a conversation's other side is, matched on the handle stored for the channel.
func (s *Service) participantContact(ctx context.Context, orgID uuid.UUID, channel string, p graphkit.Participant) *uuid.UUID {
	for _, raw := range []string{p.PublicID, p.ProfileURL, p.ProviderID} {
		_, norm, ok := models.NormalizeSocialHandle(channel, raw)
		if !ok {
			continue
		}
		ids, err := s.Repo.ContactIDsByHandle(ctx, orgID, channel, norm)
		if err != nil || len(ids) == 0 {
			continue
		}
		return &ids[0]
	}
	return nil
}

// storeMessage records a message once; inserted is true the first time it is seen.
func (s *Service) storeMessage(ctx context.Context, conv *models.SocialConversation, m *graphkit.SocialMessage, sentBy *uuid.UUID, ci *cipher.Cipher) (*models.SocialMessage, bool, error) {
	row := &models.SocialMessage{OrganizationID: conv.OrganizationID, ConversationID: conv.ID, GraphKitID: m.ID, Direction: m.Direction,
		BodySealed: s.seal(ctx, ci, m.Body), SentAt: m.SentAt, Status: m.Status, Kind: m.Kind, SentByUserID: sentBy}
	if row.Kind == "" {
		row.Kind = "message"
	}
	if row.Status == "" {
		row.Status = "sent"
	}
	inserted, err := s.Repo.UpsertMessage(ctx, row)
	return row, inserted, err
}

// applyMessage stores a message GraphKit reported and tells the workspace.
func (s *Service) applyMessage(ctx context.Context, orgID uuid.UUID, ev *graphkit.MessageEvent) error {
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return err
	}
	if ev.Conversation.LastMessage == nil {
		ev.Conversation.LastMessage = &ev.Message
	}
	if ev.Conversation.Channel == "" {
		ev.Conversation.Channel = ev.Channel
	}
	conv, err := s.storeConversation(ctx, orgID, &ev.Conversation, ci)
	if err != nil || conv == nil {
		return err
	}
	if _, _, err := s.storeMessage(ctx, conv, &ev.Message, nil, ci); err != nil {
		return err
	}
	s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialMessage}, Channel: conv.Channel,
		AccountID: conv.AccountID.String(), ConversationID: conv.ID.String(), Direction: ev.Message.Direction, Status: ev.Message.Status})
	acct, err := s.Repo.GetAccount(ctx, orgID, conv.AccountID)
	if err != nil {
		return err
	}
	return s.noteMessage(ctx, conv, acct, &ev.Message)
}

func (s *Service) toConversation(ctx context.Context, ci *cipher.Cipher, c *models.SocialConversation) *Conversation {
	return &Conversation{SocialConversation: *c, LastMessagePreview: s.open(ctx, ci, c.LastMessagePreview)}
}

// ListConversations is the workspace's conversations, latest activity first.
func (s *Service) ListConversations(ctx context.Context, orgID uuid.UUID, f ConversationFilter) (*List[*Conversation], error) {
	after, limit, err := pageArgs(f.Cursor, f.Limit)
	if err != nil {
		return nil, err
	}
	if f.AccountID != nil {
		if _, err := s.owned(ctx, orgID, *f.AccountID); err != nil {
			return nil, err
		}
	}
	if after == nil {
		s.pullConversations(ctx, orgID)
	}
	q := repository.SocialConversationFilter{AccountID: f.AccountID, Channel: f.Channel, ContactID: f.ContactID, UnreadOnly: f.UnreadOnly, Limit: limit + 1}
	if after != nil {
		q.AfterID = &after.ID
		if !after.CreatedAt.Equal(time.Unix(0, 0)) {
			q.AfterAt = &after.CreatedAt
		}
	}
	rows, err := s.Repo.ListConversations(ctx, orgID, q)
	if err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := &List[*Conversation]{Data: make([]*Conversation, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, s.toConversation(ctx, ci, &rows[i]))
	}
	if more {
		last := rows[len(rows)-1]
		at := time.Unix(0, 0)
		if last.LastMessageAt != nil {
			at = *last.LastMessageAt
		}
		out.Pagination = nextPage(true, at, last.ID)
	}
	return out, nil
}

// pullConversations fetches each live account's latest conversations from GraphKit, at most once a minute per workspace.
func (s *Service) pullConversations(ctx context.Context, orgID uuid.UUID) {
	if !s.available() {
		return
	}
	s.mu.Lock()
	if t, ok := s.pulled[orgID]; ok && s.now().Sub(t) < pullEvery {
		s.mu.Unlock()
		return
	}
	s.pulled[orgID] = s.now()
	if len(s.pulled) > 10_000 {
		s.pulled = map[uuid.UUID]time.Time{orgID: s.now()}
	}
	s.mu.Unlock()
	accounts, err := s.Repo.ListAccounts(ctx, orgID, nil, 20, false)
	if err != nil || len(accounts) == 0 {
		return
	}
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for i := range accounts {
		page, err := s.api().ListConversations(pctx, ext(orgID), graphkit.ConversationFilter{AccountID: accounts[i].GraphKitID, Limit: 50})
		if err != nil {
			log.Debug().Err(err).Str("account_id", accounts[i].ID.String()).Msg("social: pulling conversations failed")
			return
		}
		for k := range page.Data {
			if _, err := s.storeConversation(ctx, orgID, &page.Data[k], ci); err != nil {
				return
			}
		}
	}
}

// conversation is the workspace's conversation by Warmbly id.
func (s *Service) conversation(ctx context.Context, orgID, id uuid.UUID) (*models.SocialConversation, error) {
	c, err := s.Repo.GetConversation(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNotFound
	}
	return c, nil
}

// GetConversation is one conversation.
func (s *Service) GetConversation(ctx context.Context, orgID, id uuid.UUID) (*Conversation, error) {
	c, err := s.conversation(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return s.toConversation(ctx, ci, c), nil
}

// ListMessages is a conversation's messages, newest first. The first page is caught up with GraphKit first.
func (s *Service) ListMessages(ctx context.Context, orgID, id uuid.UUID, cursor string, limit int) (*List[*Message], error) {
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	conv, err := s.conversation(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if after == nil && s.available() {
		pctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		page, gerr := s.api().ListMessages(pctx, ext(orgID), conv.GraphKitID, "", 50)
		cancel()
		if gerr == nil {
			for i := range page.Data {
				if _, _, err := s.storeMessage(ctx, conv, &page.Data[i], nil, ci); err != nil {
					return nil, err
				}
			}
		}
	}
	rows, err := s.Repo.ListMessages(ctx, orgID, conv.ID, after, limit+1)
	if err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := &List[*Message]{Data: make([]*Message, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, s.toMessage(ctx, ci, &rows[i], conv.Channel))
	}
	if more {
		last := rows[len(rows)-1]
		at := last.CreatedAt
		if last.SentAt != nil {
			at = *last.SentAt
		}
		out.Pagination = nextPage(true, at, last.ID)
	}
	return out, nil
}

func (s *Service) toMessage(ctx context.Context, ci *cipher.Cipher, m *models.SocialMessage, channel string) *Message {
	return &Message{ID: m.ID, ConversationID: m.ConversationID, Channel: channel, Direction: m.Direction, Body: s.open(ctx, ci, m.BodySealed),
		SentAt: m.SentAt, Status: m.Status, Kind: m.Kind, SentBy: m.SentByUserID, CreatedAt: m.CreatedAt}
}

// ReplyRequest is POST /v1/social/conversations/:id/messages: a body, or a voice note where the channel takes one.
type ReplyRequest struct {
	Body  string               `json:"body"`
	Voice *graphkit.VoiceInput `json:"voice"`
}

// voiceReplies reports a channel whose conversations take a voice note in place of a body; Instagram sends them only as actions.
func voiceReplies(spec graphkit.SocialChannel) bool {
	return offers(spec.Actions, models.SocialActionVoiceNote) && spec.Channel != models.SocialInstagram
}

// replyInput checks a reply against the conversation's channel.
func replyInput(spec graphkit.SocialChannel, in ReplyRequest) (graphkit.MessageInput, string, error) {
	body := strings.TrimSpace(in.Body)
	if in.Voice != nil {
		if body != "" {
			return graphkit.MessageInput{}, "", invalid("Send a body or a voice note, not both.")
		}
		if !voiceReplies(spec) {
			return graphkit.MessageInput{}, "", invalid(spec.Label + " replies take no voice notes.")
		}
		v := *in.Voice
		switch v.Mode {
		case models.SocialVoiceRecording:
			if strings.TrimSpace(v.AudioID) == "" {
				return graphkit.MessageInput{}, "", invalid("voice.audio_id names the recording to send.")
			}
			v.Script = ""
		case models.SocialVoiceWritten:
			if v.Script = strings.TrimSpace(v.Script); v.Script == "" || utf8.RuneCountInString(v.Script) > spec.VoiceScriptMaxChars {
				return graphkit.MessageInput{}, "", invalid(fmt.Sprintf("voice.script must be 1 to %d characters on %s.", spec.VoiceScriptMaxChars, spec.Label))
			}
			v.AudioID = ""
		default:
			return graphkit.MessageInput{}, "", invalid("voice.mode must be recording or written.")
		}
		return graphkit.MessageInput{Voice: &v}, "Voice note", nil
	}
	limit := spec.MessageMaxChars
	if limit <= 0 {
		limit = models.SocialChannelSpecs[models.SocialLinkedIn].MessageMaxChars
	}
	if body == "" || utf8.RuneCountInString(body) > limit {
		return graphkit.MessageInput{}, "", invalid(fmt.Sprintf("body must be 1 to %d characters on %s.", limit, spec.Label))
	}
	return graphkit.MessageInput{Body: &body}, body, nil
}

// Reply sends a message in a conversation through its account.
func (s *Service) Reply(ctx context.Context, orgID, userID, id uuid.UUID, in ReplyRequest) (*Message, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	conv, err := s.conversation(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	spec, _ := s.channelSpec(ctx, conv.Channel)
	input, shown, err := replyInput(spec, in)
	if err != nil {
		return nil, err
	}
	if _, err := s.live(ctx, orgID, conv.AccountID); err != nil {
		return nil, err
	}
	sent, err := s.api().SendMessage(ctx, ext(orgID), conv.GraphKitID, input, opKey("message.send"))
	if err != nil {
		return nil, s.upstream(ctx, "social.message.send", err)
	}
	if sent.Body == "" {
		sent.Body = shown
	}
	ci, err := s.cipherFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	row, _, err := s.storeMessage(ctx, conv, sent, &userID, ci)
	if err != nil {
		return nil, err
	}
	at := sent.SentAt
	if at == nil {
		t := s.now()
		at = &t
	}
	_, _ = s.Repo.UpsertConversation(ctx, &models.SocialConversation{OrganizationID: orgID, AccountID: conv.AccountID, Channel: conv.Channel,
		GraphKitID: conv.GraphKitID, ContactID: conv.ContactID, Participant: conv.Participant, Kind: conv.Kind, UnreadCount: conv.UnreadCount,
		LastMessageAt: at, LastMessagePreview: s.seal(ctx, ci, preview(sent.Body))})
	s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialMessage}, Channel: conv.Channel,
		AccountID: conv.AccountID.String(), ConversationID: conv.ID.String(), Direction: graphkit.DirectionOutbound, Status: row.Status})
	row.CreatedAt = s.now()
	return &Message{ID: row.ID, ConversationID: conv.ID, Channel: conv.Channel, Direction: row.Direction, Body: sent.Body, SentAt: row.SentAt,
		Status: row.Status, Kind: row.Kind, SentBy: &userID, CreatedAt: row.CreatedAt}, nil
}

// MarkRead marks a conversation read at GraphKit and here.
func (s *Service) MarkRead(ctx context.Context, orgID, id uuid.UUID) error {
	if err := s.ready(ctx, orgID); err != nil {
		return err
	}
	conv, err := s.conversation(ctx, orgID, id)
	if err != nil {
		return err
	}
	if _, err := s.api().MarkConversationRead(ctx, ext(orgID), conv.GraphKitID, opKey("conversation.read")); err != nil {
		return s.upstream(ctx, "social.conversation.read", err)
	}
	if err := s.Repo.MarkConversationRead(ctx, orgID, conv.ID); err != nil {
		return err
	}
	s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialMessage}, Channel: conv.Channel,
		AccountID: conv.AccountID.String(), ConversationID: conv.ID.String(), Status: "read"})
	return nil
}
