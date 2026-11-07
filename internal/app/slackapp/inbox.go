package slackapp

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Inbox action ids; each button's value is the unibox thread id, resolved
// against the clicker's own organization.
const (
	ActionInboxReply         = "inbox_reply"
	ActionInboxReview        = "inbox_review"
	ActionInboxDraft         = "inbox_draft"
	ActionInboxInterested    = "inbox_interested"
	ActionInboxNotInterested = "inbox_not_interested"
	ActionInboxAssign        = "inbox_assign"
	CallbackInboxReplyModal  = "inbox_reply_modal"

	blockInboxState   = "inbox_state"
	blockInboxActions = "inbox_actions"

	inboxExcerptRunes = 1500
	inboxMessageTTL   = 7 * 24 * time.Hour
)

// UniboxThreads reads stored conversations; satisfied by repository.UniboxRepository.
type UniboxThreads interface {
	GetByThread(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error)
	GetByIDForOrg(ctx context.Context, orgID, id uuid.UUID) (*models.EmailMessageStoreData, uuid.UUID, error)
	ListThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string) ([]models.MiniCategory, error)
}

// TaskLookup and CampaignLookup name the campaign a reply answers; satisfied
// by repository.TaskRepository and repository.CampaignRepository.
type TaskLookup interface {
	GetTaskByMessageID(ctx context.Context, messageID string) (*repository.Task, error)
	GetCampaignTask(ctx context.Context, taskID uuid.UUID) (*repository.CampaignTask, error)
}

type CampaignLookup interface {
	GetByID(ctx context.Context, campaignID uuid.UUID) (*models.Campaign, error)
}

// UserLookup names the member who sent a reply; satisfied by the user repository.
type UserLookup interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
}

// InboxDeps builds an InboxPoster. Only Integrations and Repo are required.
type InboxDeps struct {
	Integrations Integrations
	Repo         repository.SlackRepository
	Redis        *redis.Client
	Threads      UniboxThreads
	Tasks        TaskLookup
	Campaigns    CampaignLookup
	Users        UserLookup
}

// InboxPoster mirrors the unified inbox into the workspace's Slack inbox
// channel. It needs nothing the consumer lacks.
type InboxPoster struct {
	d      InboxDeps
	client *Client
	guard  *guard

	connMu sync.Mutex
	conns  map[uuid.UUID]cachedConn
}

// cachedConn spares every inbox arrival a connection lookup.
type cachedConn struct {
	at   time.Time
	conn *models.IntegrationConnection
}

const inboxConnTTL = 30 * time.Second

func NewInboxPoster(d InboxDeps) *InboxPoster {
	return &InboxPoster{d: d, client: NewClient(), guard: newGuard(d.Redis), conns: map[uuid.UUID]cachedConn{}}
}

func (p *InboxPoster) connection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error) {
	p.connMu.Lock()
	if c, ok := p.conns[orgID]; ok && time.Since(c.at) < inboxConnTTL {
		p.connMu.Unlock()
		return c.conn, nil
	}
	p.connMu.Unlock()
	conn, err := p.d.Integrations.SlackConnection(ctx, orgID)
	if err != nil {
		return nil, err
	}
	p.connMu.Lock()
	if len(p.conns) > 5000 {
		p.conns = map[uuid.UUID]cachedConn{}
	}
	p.conns[orgID] = cachedConn{at: time.Now(), conn: conn}
	p.connMu.Unlock()
	return conn, nil
}

// slackSendKey marks a send made from Slack, which posts its own receipt.
type slackSendKey struct{}

func withSlackSend(ctx context.Context) context.Context {
	return context.WithValue(ctx, slackSendKey{}, true)
}

func isSlackSend(ctx context.Context) bool {
	v, _ := ctx.Value(slackSendKey{}).(bool)
	return v
}

// InboundMessage mirrors one stored arrival in the background.
func (p *InboxPoster) InboundMessage(_ context.Context, orgID uuid.UUID, account *models.Email, msg *models.EmailMessageStoreData) {
	if p == nil || account == nil || msg == nil || msg.ThreadID == "" {
		return
	}
	acct, m := *account, *msg
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Str("panic", fmt.Sprint(r)).Msg("slack: inbox mirror panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := p.deliverInbound(ctx, orgID, &acct, &m); err != nil {
			log.Warn().Err(err).Str("org_id", orgID.String()).Msg("slack: inbox mirror failed")
		}
	}()
}

func (p *InboxPoster) deliverInbound(ctx context.Context, orgID uuid.UUID, acct *models.Email, msg *models.EmailMessageStoreData) error {
	conn, err := p.connection(ctx, orgID)
	if err != nil || conn == nil {
		return err
	}
	st := settingsFrom(conn)
	if st.InboxChannel == "" {
		return nil
	}
	verdict := replyVerdict(ctx, orgID, msg)
	if !inboxWants(st.InboxScope, acct, msg, verdict) {
		return nil
	}
	if !p.guard.first(ctx, "slack:inbox:msg:"+orgID.String()+":"+msg.ID.String(), inboxMessageTTL) {
		return nil
	}
	release, ok := p.lockThread(ctx, orgID, msg.ThreadID)
	if !ok {
		return nil
	}
	defer release()

	token, err := p.d.Integrations.SlackBotToken(ctx, orgID, conn.ID)
	if err != nil {
		return err
	}
	info := p.describe(ctx, orgID, acct, msg, verdict)

	existing, err := p.d.Repo.GetInboxThread(ctx, orgID, msg.ThreadID)
	if err != nil {
		return err
	}
	if existing != nil && existing.ConnectionID == conn.ID && sameChannel(existing.ChannelID, st.InboxChannel) {
		reply := inboxFollowUp(info)
		reply.Channel, reply.ThreadTS = existing.ChannelID, existing.ThreadTS
		if _, err := p.client.PostMessage(ctx, token, reply); err == nil {
			return nil
		} else if !IsAPIError(err, "thread_not_found", "message_not_found", "channel_not_found", "not_in_channel", "is_archived") {
			return err
		}
	}

	parent := inboxParent(info)
	parent.Channel = st.InboxChannel
	ts, channel, err := p.client.PostMessageIn(ctx, token, parent)
	if err != nil {
		return err
	}
	if channel == "" {
		channel = st.InboxChannel
	}
	_, err = p.d.Repo.UpsertInboxThread(ctx, &models.SlackInboxThread{
		OrganizationID: orgID, ConnectionID: conn.ID, ChannelID: channel, ThreadTS: ts, UniboxThreadID: msg.ThreadID,
	})
	return err
}

// lockThread serializes posts per conversation so two arrivals cannot both
// start a Slack thread.
func (p *InboxPoster) lockThread(ctx context.Context, orgID uuid.UUID, threadID string) (func(), bool) {
	key := "slack:inbox:thread:" + orgID.String() + ":" + threadID
	for i := 0; i < 20; i++ {
		if release, ok := p.guard.lock(ctx, key, time.Minute); ok {
			return release, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, false
}

// sameChannel compares a stored channel id with the setting, which a legacy
// "#name" cannot be compared against.
func sameChannel(stored, setting string) bool {
	return strings.HasPrefix(setting, "#") || stored == setting
}

// inboxWants applies the inbox scope: never the mailbox's own mail; "replies"
// keeps only a person's reply, never an auto-reply, out-of-office or bounce.
func inboxWants(scope string, acct *models.Email, msg *models.EmailMessageStoreData, verdict replyclassify.Result) bool {
	sender := senderAddress(msg.FromAddr)
	if sender == "" {
		return false
	}
	for _, own := range []string{acct.Email, acct.SendFrom()} {
		if own != "" && strings.EqualFold(sender, strings.TrimSpace(own)) {
			return false
		}
	}
	if scope == models.SlackInboxScopeAll {
		return true
	}
	if len(msg.InReplyTo) == 0 {
		return false
	}
	if replyclassify.IsDeliveryFailure(classifyInput(msg)) {
		return false
	}
	return !replyclassify.IsAutomated(verdict.Class)
}

// replyVerdict reads msg the way the campaign does: headers and an opt-out
// first, then the tagger's stored verdict, then the offline keywords.
func replyVerdict(ctx context.Context, orgID uuid.UUID, msg *models.EmailMessageStoreData) replyclassify.Result {
	in := classifyInput(msg)
	in.OrganizationID, in.MessageID = orgID, msg.MessageID
	return replyclassify.ClassifyKnown(ctx, in)
}

func classifyInput(msg *models.EmailMessageStoreData) replyclassify.Input {
	h := replyclassify.FlagHeaders(msg.Flags)
	if len(msg.FromAddr) > 0 {
		h["From"] = msg.FromAddr
	}
	if msg.Subject != "" {
		h["Subject"] = []string{msg.Subject}
	}
	body := msg.BodyText
	if strings.TrimSpace(body) == "" {
		body = msg.Snippet
	}
	return replyclassify.Input{Headers: h, Subject: msg.Subject, BodyText: body}
}

func senderAddress(from []string) string {
	if len(from) == 0 {
		return ""
	}
	if a, err := mail.ParseAddress(from[0]); err == nil {
		return a.Address
	}
	return strings.Trim(strings.TrimSpace(from[0]), "<>")
}

func senderName(from []string) string {
	if len(from) == 0 {
		return ""
	}
	if a, err := mail.ParseAddress(from[0]); err == nil {
		return a.Name
	}
	return ""
}

// inboundInfo is what an inbox card shows, all of it already sanitized.
type inboundInfo struct {
	ThreadID string
	From     string
	Subject  string
	Mailbox  string
	Campaign string
	Intent   string
	Excerpt  string
}

func (p *InboxPoster) describe(ctx context.Context, orgID uuid.UUID, acct *models.Email, msg *models.EmailMessageStoreData, verdict replyclassify.Result) inboundInfo {
	in := classifyInput(msg)
	info := inboundInfo{
		ThreadID: msg.ThreadID,
		From:     fromLine(msg.FromAddr),
		Subject:  sanitizeInbound(truncateRunes(strings.TrimSpace(msg.Subject), 200)),
		Mailbox:  escapeMrkdwn(acct.Email),
		Excerpt:  quoteBlock(sanitizeInbound(truncateRunes(cleanExcerpt(in.BodyText), inboxExcerptRunes))),
	}
	var tags []string
	switch verdict.Class {
	case replyclassify.ClassPositive:
		tags = append(tags, "Positive")
	case replyclassify.ClassNegative:
		tags = append(tags, "Negative")
	case replyclassify.ClassUnsubscribe:
		tags = append(tags, "Unsubscribe")
	}
	if p.d.Threads != nil {
		if labels, err := p.d.Threads.ListThreadLabels(ctx, orgID, msg.ThreadID); err == nil {
			for _, l := range labels {
				tags = append(tags, escapeMrkdwn(l.Title))
			}
		}
	}
	info.Intent = strings.Join(tags, ", ")
	info.Campaign = p.campaignName(ctx, orgID, msg.InReplyTo)
	return info
}

// campaignName names the org's campaign whose send this message answers.
func (p *InboxPoster) campaignName(ctx context.Context, orgID uuid.UUID, inReplyTo []string) string {
	if p.d.Tasks == nil || p.d.Campaigns == nil {
		return ""
	}
	for _, mid := range inReplyTo {
		mid = strings.Trim(strings.TrimSpace(mid), "<>")
		if mid == "" {
			continue
		}
		task, err := p.d.Tasks.GetTaskByMessageID(ctx, mid)
		if err != nil || task == nil || task.TaskType != "campaign" {
			continue
		}
		ct, err := p.d.Tasks.GetCampaignTask(ctx, task.ID)
		if err != nil || ct == nil || ct.CampaignID == nil {
			continue
		}
		c, err := p.d.Campaigns.GetByID(ctx, *ct.CampaignID)
		if err != nil || c == nil || c.OrganizationID == nil || *c.OrganizationID != orgID {
			continue
		}
		return escapeMrkdwn(truncateRunes(c.Name, 120))
	}
	return ""
}

func fromLine(from []string) string {
	addr := sanitizeInbound(truncateRunes(senderAddress(from), 200))
	name := sanitizeInbound(truncateRunes(strings.TrimSpace(senderName(from)), 100))
	if name == "" {
		return "*" + addr + "*"
	}
	return "*" + name + "* (" + addr + ")"
}

var (
	broadcastMention = regexp.MustCompile(`(?i)@(channel|here|everyone)\b`)
	blankRuns        = regexp.MustCompile(`\n{3,}`)
)

// sanitizeInbound makes attacker-controlled mail text inert in mrkdwn: the
// reserved characters are escaped (so no <!channel>, <@U…> or <url|label>),
// and a bare @channel/@here/@everyone is broken with a zero-width space.
func sanitizeInbound(s string) string {
	s = escapeMrkdwn(s)
	return broadcastMention.ReplaceAllString(s, "@​$1")
}

// cleanExcerpt drops quoted history and collapses blank runs.
func cleanExcerpt(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if stripped := strings.TrimSpace(replyclassify.StripQuoted(body)); stripped != "" {
		body = stripped
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(body, "\n\n"))
}

func quoteBlock(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}

// uniboxThreadURL mirrors advanced.UniboxThreadLink: the conversation itself.
func uniboxThreadURL(threadID string) string {
	return appURL("/app/unibox/all/" + url.PathEscape(threadID))
}

func inboxActions(threadID string) Block {
	b := actionsBlock(
		actionButton("Reply", ActionInboxReply, threadID, "primary"),
		actionButton("Draft with AI", ActionInboxDraft, threadID, ""),
		actionButton("Interested", ActionInboxInterested, threadID, ""),
		actionButton("Not interested", ActionInboxNotInterested, threadID, ""),
		actionButton("Assign to me", ActionInboxAssign, threadID, ""),
		urlButton("Open in Warmbly", uniboxThreadURL(threadID)),
	)
	b["block_id"] = blockInboxActions
	return b
}

func inboxParent(info inboundInfo) Message {
	var fields []any
	add := func(label, v string) {
		if v != "" {
			fields = append(fields, mrkdwnText("*"+label+"*\n"+v))
		}
	}
	add("Subject", info.Subject)
	add("Mailbox", info.Mailbox)
	add("Campaign", info.Campaign)
	add("Intent", info.Intent)
	head := Block{"type": "section", "text": mrkdwnText("New reply from " + info.From)}
	if len(fields) > 0 {
		head["fields"] = fields
	}
	var excerpt Block
	if info.Excerpt != "" {
		excerpt = sectionBlock(info.Excerpt)
	}
	return Message{
		Text:   "New reply: " + truncateRunes(info.Subject, 150),
		Blocks: blocks(head, excerpt, inboxActions(info.ThreadID)),
	}
}

func inboxFollowUp(info inboundInfo) Message {
	text := "New message from " + info.From
	if info.Excerpt != "" {
		text += "\n" + info.Excerpt
	}
	return Message{
		Text: "New message in this conversation",
		Blocks: blocks(
			sectionBlock(text),
			buttonsBlock(
				actionButton("Reply", ActionInboxReply, info.ThreadID, "primary"),
				actionButton("Draft with AI", ActionInboxDraft, info.ThreadID, ""),
			),
		),
	}
}

// ReplyQueued mirrors a reply a member queued from Warmbly into the
// conversation's Slack thread. Sends made from Slack post their own receipt.
func (p *InboxPoster) ReplyQueued(ctx context.Context, orgID, userID uuid.UUID, threadID, body string, scheduledAt time.Time) {
	if p == nil || threadID == "" || isSlackSend(ctx) {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Str("panic", fmt.Sprint(r)).Msg("slack: reply mirror panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), shortTaskTime)
		defer cancel()
		p.postSent(ctx, orgID, userID, threadID, body, scheduledAt, "")
	}()
}

// postSent writes "Sent by" into the conversation's thread; slackUser, when
// set, is the Slack member who sent it.
func (p *InboxPoster) postSent(ctx context.Context, orgID, userID uuid.UUID, threadID, body string, scheduledAt time.Time, slackUser string) {
	mapping, err := p.d.Repo.GetInboxThread(ctx, orgID, threadID)
	if err != nil || mapping == nil {
		return
	}
	token, err := p.d.Integrations.SlackBotToken(ctx, orgID, mapping.ConnectionID)
	if err != nil {
		return
	}
	who := slackUser
	if who != "" {
		who = "<@" + who + ">"
	} else if link, err := p.d.Repo.GetLinkForUser(ctx, orgID, userID); err == nil && link != nil && link.ConnectionID == mapping.ConnectionID {
		who = "<@" + link.SlackUserID + ">"
	} else if p.d.Users != nil {
		if u, err := p.d.Users.GetUser(ctx, userID); err == nil && u != nil {
			who = escapeMrkdwn(strings.TrimSpace(u.FirstName + " " + u.LastName))
		}
	}
	if who == "" {
		who = "a teammate"
	}
	lead := "Sent by " + who
	if time.Until(scheduledAt) > 2*time.Minute {
		lead = "Scheduled by " + who + " for <!date^" + fmt.Sprint(scheduledAt.Unix()) + "^{date_short_pretty} at {time}|" + scheduledAt.UTC().Format("2 Jan 15:04 UTC") + ">"
	}
	text := lead
	if excerpt := quoteBlock(escapeMrkdwn(truncateRunes(strings.TrimSpace(body), 600))); excerpt != "" {
		text += "\n" + excerpt
	}
	if _, err := p.client.PostMessage(ctx, token, Message{
		Channel: mapping.ChannelID, ThreadTS: mapping.ThreadTS, Text: "Reply sent", Blocks: blocks(sectionBlock(text)),
	}); err != nil {
		log.Warn().Err(err).Msg("slack: reply receipt failed")
	}
}
