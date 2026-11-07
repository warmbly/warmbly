package notification

import (
	"context"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
)

// Email digest engine. Email-channel notifications never send at creation:
// notifyOne queues them as pending rows with a due time from the user's
// cadence, and this loop flushes what is still unread when due. One flush
// sends at most one email per user (their pending rows bundle into a digest);
// an event shared by several members that is the only thing pending for each
// of them goes out once, with every recipient in To. Reading a
// notification in-app cancels its pending email (repo.MarkRead), which is
// what keeps active users from ever being emailed about things they saw.
const (
	emailFlushEvery    = 30 * time.Second
	emailRetryDelay    = 10 * time.Minute
	emailMaxAttempts   = 3
	emailSendTimeout   = 10 * time.Second
	emailFlushTimeout  = 2 * time.Minute
	emailDailyCapValue = 25
)

// EmailDailyCap is the max non-security notification emails one user receives
// per rolling 24h — a backstop on top of the bundling window (which already
// bounds the channel: there is no per-event mode, the floor is 30 minutes).
// NOTIFICATION_EMAIL_DAILY_CAP overrides; 0 means unlimited.
func EmailDailyCap() int {
	if raw := os.Getenv("NOTIFICATION_EMAIL_DAILY_CAP"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return emailDailyCapValue
}

// emailHold maps the user's bundling window to the pending hold. Security
// sign-in alerts always go out on the next tick regardless of the window.
func emailHold(minutes int, category models.NotificationCategory) time.Duration {
	if category == models.NotifSecuritySignIn {
		return 0
	}
	m := min(max(minutes, config.NotificationEmailWindowMinMinutes), config.NotificationEmailWindowMaxMinutes)
	return time.Duration(m) * time.Minute
}

// overEmailBudget reports whether a user has spent their rolling 24h email
// budget. Errors count as in-budget — the cap is a cost guard, not a gate
// worth losing alerts over.
func (s *service) overEmailBudget(ctx context.Context, userID uuid.UUID) bool {
	limit := EmailDailyCap()
	if limit <= 0 {
		return false
	}
	sent, err := s.repo.CountEmailedSince(ctx, userID, time.Now().Add(-24*time.Hour))
	return err == nil && sent >= limit
}

func (s *service) emailFlushLoop() {
	ticker := time.NewTicker(emailFlushEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.flushDueEmails()
	}
}

func (s *service) flushDueEmails() {
	ctx, cancel := context.WithTimeout(context.Background(), emailFlushTimeout)
	defer cancel()

	claimed, err := s.repo.ClaimDueEmails(ctx)
	if err != nil || len(claimed) == 0 {
		return
	}

	// Eligible first: a row that will not send must not keep a shared event from going out once.
	groups, byUser := partitionEmails(s.keepEligibleEmailMessages(ctx, claimed))
	for _, rows := range groups {
		s.sendGroupEmail(ctx, rows)
	}
	for userID, rows := range byUser {
		s.sendUserEmail(ctx, userID, rows)
	}
}

// partitionEmails sends a shared event once only when it is all its recipients have pending; otherwise it joins their digests.
func partitionEmails(claimed []models.Notification) ([][]models.Notification, map[uuid.UUID][]models.Notification) {
	pending := map[uuid.UUID]int{}
	byGroup := map[string][]models.Notification{}
	for _, n := range claimed {
		pending[n.UserID]++
		if n.GroupKey != "" && n.OrganizationID != nil {
			key := n.OrganizationID.String() + "|" + n.GroupKey
			byGroup[key] = append(byGroup[key], n)
		}
	}
	grouped := map[uuid.UUID]bool{}
	var groups [][]models.Notification
	for _, rows := range byGroup {
		users := map[uuid.UUID]int{}
		for _, n := range rows {
			users[n.UserID]++
		}
		alone := len(users) >= 2
		for user, count := range users {
			alone = alone && pending[user] == count
		}
		if !alone {
			continue
		}
		groups = append(groups, rows)
		for _, n := range rows {
			grouped[n.ID] = true
		}
	}
	byUser := map[uuid.UUID][]models.Notification{}
	for _, n := range claimed {
		if !grouped[n.ID] {
			byUser[n.UserID] = append(byUser[n.UserID], n)
		}
	}
	return groups, byUser
}

// sendGroupEmail delivers one shared event to all its recipients in a single
// message, addresses together in To, so teammates see who else was told. Rows
// arrive already re-checked for membership (it can end during the hold); a
// recipient over their email budget is dropped to skipped, and the in-app feed
// remains their record.
func (s *service) sendGroupEmail(ctx context.Context, rows []models.Notification) {
	to := make([]string, 0, len(rows))
	seen := map[string]bool{}
	kept := make([]models.Notification, 0, len(rows))
	dropped := make([]uuid.UUID, 0)
	budgetOK := map[uuid.UUID]bool{}
	for _, n := range rows {
		if _, checked := budgetOK[n.UserID]; !checked {
			budgetOK[n.UserID] = !s.overEmailBudget(ctx, n.UserID)
		}
		if !budgetOK[n.UserID] {
			dropped = append(dropped, n.ID)
			continue
		}
		kept = append(kept, n)
		if user, err := s.users.GetUser(ctx, n.UserID); err == nil && user != nil && user.Email != "" && !seen[user.Email] {
			seen[user.Email] = true
			to = append(to, user.Email)
		}
	}
	_ = s.repo.SkipEmails(ctx, dropped)

	ids := notifIDs(kept)
	if len(to) == 0 {
		_ = s.repo.MarkEmailed(ctx, ids) // nobody resolvable — don't retry forever
		return
	}
	first := kept[0]
	html, gerr := templates.GenerateNotificationHTML(first.Title, emailBody(first), absoluteLink(first.Link), "")
	if gerr != nil {
		_ = s.repo.RequeueEmails(ctx, ids, emailRetryDelay, emailMaxAttempts)
		return
	}
	s.settleSend(ctx, ids, to, first.Title, html)
}

// sendUserEmail bundles everything pending for one user: a lone row renders
// as itself, several become a digest listing each item. Over the email
// budget, non-security rows skip (the feed keeps them) and only security
// sign-in alerts still send.
func (s *service) sendUserEmail(ctx context.Context, userID uuid.UUID, rows []models.Notification) {
	if s.overEmailBudget(ctx, userID) {
		var capped []uuid.UUID
		kept := rows[:0:0]
		for _, n := range rows {
			if n.Category == models.NotifSecuritySignIn {
				kept = append(kept, n)
			} else {
				capped = append(capped, n.ID)
			}
		}
		_ = s.repo.SkipEmails(ctx, capped)
		if len(kept) == 0 {
			return
		}
		rows = kept
	}

	ids := notifIDs(rows)
	user, err := s.users.GetUser(ctx, userID)
	if err != nil || user == nil || user.Email == "" {
		_ = s.repo.MarkEmailed(ctx, ids)
		return
	}

	var subject, html string
	var gerr error
	if len(rows) == 1 {
		n := rows[0]
		subject = n.Title
		html, gerr = templates.GenerateNotificationHTML(n.Title, emailBody(n), absoluteLink(n.Link), "")
	} else {
		sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
		items := make([]templates.DigestItem, 0, len(rows))
		for _, n := range rows {
			items = append(items, templates.DigestItem{Title: n.Title, Body: emailBody(n), URL: absoluteLink(n.Link)})
		}
		subject = templates.DigestSubject(len(rows))
		html, gerr = templates.GenerateDigestHTML(len(rows), items)
	}
	if gerr != nil {
		_ = s.repo.RequeueEmails(ctx, ids, emailRetryDelay, emailMaxAttempts)
		return
	}
	s.settleSend(ctx, ids, []string{user.Email}, subject, html)
}

func (s *service) settleSend(ctx context.Context, ids []uuid.UUID, to []string, subject, html string) {
	sctx, cancel := context.WithTimeout(ctx, emailSendTimeout)
	defer cancel()
	if err := s.email.Send(sctx, to, nil, nil, subject, html); err != nil {
		_ = s.repo.RequeueEmails(ctx, ids, emailRetryDelay, emailMaxAttempts)
		return
	}
	_ = s.repo.MarkEmailed(ctx, ids)
}

func notifIDs(rows []models.Notification) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, n := range rows {
		ids = append(ids, n.ID)
	}
	return ids
}

// keepEligibleEmailMessages drops rows that may no longer send and holds a user's eligible rows, bar sign-in alerts, while any of theirs could not be checked.
func (s *service) keepEligibleEmailMessages(ctx context.Context, rows []models.Notification) []models.Notification {
	eligible := rows[:0:0]
	var skipped, retry []uuid.UUID
	unsure := map[uuid.UUID]bool{}
	for _, n := range rows {
		allowed, err := s.recipientEligibility(ctx, n.UserID, n.OrganizationID, n.Category)
		if err == nil && allowed {
			allowed, err = s.messageEligibility(ctx, n.Category, n.UniboxEmailID)
		}
		if err != nil {
			unsure[n.UserID] = true
			retry = append(retry, n.ID)
		} else if allowed {
			eligible = append(eligible, n)
		} else {
			skipped = append(skipped, n.ID)
		}
	}
	kept := eligible[:0:0]
	var held []uuid.UUID
	for _, n := range eligible {
		if unsure[n.UserID] && n.Category != models.NotifSecuritySignIn {
			held = append(held, n.ID)
		} else {
			kept = append(kept, n)
		}
	}
	if len(held) > 0 {
		if err := s.repo.DeferEmails(ctx, held, emailRetryDelay); err != nil {
			log.Printf("notification: defer %d emails failed, stale-claim recovery reclaims them: %v", len(held), err)
		}
	}
	if len(skipped) > 0 {
		_ = s.repo.SkipEmails(ctx, skipped)
	}
	if len(retry) > 0 {
		if err := s.repo.RequeueEmails(ctx, retry, emailRetryDelay, emailMaxAttempts); err != nil {
			log.Printf("notification: requeue %d emails failed, stale-claim recovery reclaims them: %v", len(retry), err)
		}
	}
	return kept
}

// emailBody keeps text an outside sender wrote, such as a reply's subject, out
// of mail Warmbly sends; the in-app feed still shows it.
func emailBody(n models.Notification) string {
	switch n.Category {
	case models.NotifInboundReply, models.NotifInboundOOO:
		return "Open the conversation in your inbox to read it."
	}
	return n.Body
}

// absoluteLink turns a dashboard-relative link into a clickable URL.
func absoluteLink(link string) string {
	if link == "" || link[0] != '/' {
		return link
	}
	return config.AppBaseURL() + link
}
