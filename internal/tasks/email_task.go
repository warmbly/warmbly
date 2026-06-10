package tasks

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func (s *tasksService) HandleEmailTask(task *proto.ProcessTask) *errx.Error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// STEP 1: Parse task ID
	taskID, err := uuid.Parse(task.TaskId)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.BadRequest, "invalid task ID")
	}

	executionKey := "warmup:" + taskID.String()
	executionStatus := "failed"
	// lane distinguishes a normal user-enabled warmup send ("warmup") from a
	// health-check send kept flowing only because the mailbox backs a live
	// campaign ("health_check"). Set once the account is loaded; the defer
	// reads it at completion time.
	lane := "warmup"
	if s.advanced != nil {
		duplicate, xerr := s.advanced.StartTaskExecution(ctx, taskID, executionKey, map[string]interface{}{
			"task_type": "warmup",
		})
		if xerr != nil {
			return xerr
		}
		if duplicate {
			return nil
		}
		defer func() {
			_ = s.advanced.CompleteTaskExecution(ctx, taskID, executionKey, executionStatus, map[string]interface{}{
				"task_type": "warmup",
				"lane":      lane,
			})
		}()
	}

	// STEP 2: Load task record
	taskRecord, err := s.taskRepo.GetTask(ctx, taskID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if taskRecord == nil {
		return errx.ErrNotFound
	}

	if taskRecord.Status != "pending" {
		log.Info().
			Str("task_id", taskID.String()).
			Str("status", taskRecord.Status).
			Msg("warmup task skipped: task not in pending state")
		executionStatus = "completed"
		return nil
	}

	// STEP 3: Load email account
	account, xerr := s.emailRepo.GetByID(ctx, taskRecord.EmailAccountID)
	if xerr != nil {
		return xerr
	}
	if account == nil {
		return errx.ErrNotFound
	}
	// Keep the warmup chain alive while the mailbox is actively warming OR
	// while it backs a live campaign (the low-volume health-check lane). Once
	// neither holds, the chain is allowed to wind down.
	activelyWarming := account.IsWarmingActive()
	inCampaign := false
	if !activelyWarming {
		inCampaign = s.accountInActiveCampaign(ctx, account.ID)
	}
	if account.Status != "active" || (!activelyWarming && !inCampaign) {
		if s.warmupHealth != nil {
			if account.Status == "active" && account.OrganizationID != nil && s.featureGate != nil {
				canWarmup, _ := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
				if canWarmup {
					_ = s.warmupHealth.EnsurePoolMembershipWithRole(ctx, account.ID, s.resolveWarmupPoolType(ctx, account), "recipient_only")
				} else {
					_ = s.warmupHealth.RemovePoolMembership(ctx, account.ID, s.resolveWarmupPoolType(ctx, account))
				}
			} else {
				_ = s.warmupHealth.RemovePoolMembership(ctx, account.ID, s.resolveWarmupPoolType(ctx, account))
			}
		}
		_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
		executionStatus = "completed"
		return nil
	}
	if !activelyWarming && inCampaign {
		lane = "health_check"
		log.Info().
			Str("task_id", taskID.String()).
			Str("email_account_id", account.ID.String()).
			Msg("warmup health-check send (mailbox in active campaign, warmup off)")
	}

	// STEP 3.5: Check if organization can use warmup (only paid orgs)
	if s.featureGate != nil && account.OrganizationID != nil {
		canWarmup, _ := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
		if !canWarmup {
			if s.warmupHealth != nil {
				_ = s.warmupHealth.RemovePoolMembership(ctx, account.ID, s.resolveWarmupPoolType(ctx, account))
			}
			// Organization cannot use warmup - skip this task
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_no_warmup_access")
			executionStatus = "completed"
			return nil
		}
	}

	poolType := s.resolveWarmupPoolType(ctx, account)
	if s.warmupHealth != nil {
		if err := s.warmupHealth.EnsurePoolMembershipWithRole(ctx, account.ID, poolType, "sender_receiver"); err != nil {
			return err
		}

		canParticipate, _, err := s.warmupHealth.CanParticipate(ctx, account.ID, poolType)
		if err != nil {
			return err
		}
		if !canParticipate {
			_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_warmup_protected")
			s.scheduleWarmupRecovery(ctx, account.ID, poolType)
			executionStatus = "completed"
			return nil
		}
	}

	// STEP 4: Mark task as active (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "active"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 5: Select warmup partner from pool
	partner, err := s.selectWarmupPartner(ctx, *account)
	if err != nil {
		_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_warmup_protected")
		nextTime, scheduleErr := s.scheduler.CalculateNextWarmupTime(ctx, account.ID)
		if scheduleErr != nil {
			nextTime = warmupPartnerRecheckTime()
		}
		if nextTime.Before(time.Now().Add(4 * time.Hour)) {
			nextTime = warmupPartnerRecheckTime()
		}
		if createErr := s.createWarmupTask(ctx, account.ID, nextTime); createErr != nil {
			log.Warn().Err(createErr).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("Failed to reschedule warmup task after partner exhaustion")
		}
		executionStatus = "completed"
		return nil
	}

	// STEP 6: Determine if this should be a reply or a new warmup email.
	// When replying, the body is drawn from the same conversation theme as
	// the original send so the thread stays topically coherent.
	replyRate := account.WarmupReplyRate
	shouldReply := rand.Float64()*100 < float64(replyRate)
	var subject, emailBody, conversationTheme, contentSource string
	var conversationID *uuid.UUID
	var inReplyTo string

	if shouldReply {
		candidate, replyErr := s.warmupRepo.GetLatestReplyCandidate(ctx, partner.ID, account.ID)
		if replyErr == nil && candidate != nil && candidate.MessageID != "" {
			inReplyTo = candidate.MessageID
			subject = strings.TrimSpace(candidate.Subject)
			if subject == "" {
				subject = generateWarmupSubject()
			}
			// "Re:" is legitimate here — this is a genuine reply with a real
			// In-Reply-To header (synthesizeWarmupSubject no longer fabricates
			// "Re:" on first-touch sends).
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
			conv := conversationForTheme(candidate.ConversationTheme)
			conversationTheme = conv.Theme
			contentSource = models.WarmupContentSourceStatic
			emailBody = GenerateConversationEmail(conv, *account, true)
		} else {
			shouldReply = false
		}
	}

	// STEP 7: Build a new warmup message when not replying. Content comes from
	// the AI bank (segment-aware) when enabled, else the static library.
	if !shouldReply {
		content := s.pickNewWarmupContent(ctx, *account)
		subject = content.subject
		emailBody = content.body
		conversationTheme = content.theme
		contentSource = content.contentSource
		conversationID = content.conversationID
	}

	// STEP 7.5: Content-safety lint. Warmup mail must look unremarkable; if the
	// chosen content trips the lint (most likely AI drift) fall back to clean
	// static content so we never send spammy-looking warmup.
	if err := lintWarmupContent(subject, emailBody, shouldReply); err != nil {
		log.Warn().Err(err).
			Str("email_account_id", account.ID.String()).
			Str("content_source", contentSource).
			Msg("warmup content failed lint; falling back to static")
		conv := randomWarmupConversation()
		conversationTheme = conv.Theme
		fallbackID := conv.ID
		conversationID = &fallbackID
		contentSource = models.WarmupContentSourceStatic
		subject = generateWarmupSubject()
		emailBody = GenerateConversationEmail(conv, *account, false)
		if err2 := lintWarmupContent(subject, emailBody, false); err2 != nil {
			subject = "Quick note"
			emailBody = GenerateConversationEmail(Conversation{
				Theme:       "checkin",
				Description: "Just checking in — hope all is well.",
				Messages:    []string{"How have things been lately?"},
			}, *account, false)
		}
	}

	// STEP 9: Generate Message-ID
	messageID := generateMessageID(account.Email)
	// Persist it now so the reply path (GetLatestReplyCandidate, which filters
	// message_id <> '') can find this send as a thread parent on a later turn.
	// Without this the warmup reply/threading path never fires.
	if err := s.taskRepo.UpdateTaskMessageID(ctx, taskID, messageID); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("Failed to persist warmup task message_id")
	}

	// STEP 9.5: Generate warmup verification token
	var warmupTokenStr string
	warmupToken := uuid.New()
	tokenRecord := &models.WarmupToken{
		Token:              warmupToken,
		TaskID:             taskID,
		SenderAccountID:    account.ID,
		RecipientAccountID: partner.ID,
		ConversationTheme:  conversationTheme,
		ContentSource:      contentSource,
		ConversationID:     conversationID,
		ExpiresAt:          time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.warmupRepo.CreateWarmupToken(ctx, tokenRecord); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("Failed to create warmup token")
	} else {
		warmupTokenStr = warmupToken.String()
	}

	// STEP 10: Send warmup email to worker via Kafka
	emailMsg := EmailMessage{
		From:        account.Email,
		To:          []string{partner.Email},
		Subject:     subject,
		BodyHTML:    "", // Warmup emails are plaintext only
		BodyPlain:   emailBody,
		InReplyTo:   inReplyTo,
		MessageID:   messageID,
		IsWarmup:    true,
		Tracking:    nil, // No tracking for warmup
		WarmupToken: warmupTokenStr,
	}

	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		s.taskRepo.RecordTaskFailure(ctx, taskID, "Send failed", err.Error())
		if s.advanced != nil {
			_ = s.advanced.CaptureTaskDeadLetter(ctx, taskID, "warmup", map[string]interface{}{
				"partner_email": partner.Email,
			}, err.Error(), 1)
			_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "dead_lettered")
		}
		return nil
	}

	// STEP 11: Update task record
	taskRecord.MessageID = messageID
	taskRecord.Status = "completed"

	// STEP 12: Update warmup statistics
	if err := s.warmupRepo.IncrementDailyCount(ctx, account.ID, time.Now()); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("Failed to increment warmup daily count")
	}
	// Track replies separately so warmup reply analytics (emails_replied) is no
	// longer always zero. Conversational replies are a healthy-traffic signal.
	if shouldReply {
		if err := s.warmupRepo.IncrementReplyCount(ctx, account.ID, time.Now()); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("Failed to increment warmup reply count")
		}
	}

	// STEP 13: Mark task completed (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 14: Publish events
	s.publishWarmupEmailSentEvent(ctx, taskRecord, account, partner, shouldReply)

	// STEP 15: Calculate next warmup time and create new task
	nextTime, err := s.scheduler.CalculateNextWarmupTime(ctx, account.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("Failed to calculate next warmup time")
		return nil
	}

	if err := s.createWarmupTask(ctx, account.ID, nextTime); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("Failed to create next warmup task")
	}

	executionStatus = "completed"
	return nil
}

// accountInActiveCampaign reports whether the mailbox currently backs at
// least one active campaign. Failing closed on error keeps the health-check
// lane conservative — a transient DB error just lets the chain wind down if
// warmup is otherwise off.
func (s *tasksService) accountInActiveCampaign(ctx context.Context, accountID uuid.UUID) bool {
	if s.campaignRepo == nil {
		return false
	}
	count, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, accountID)
	if err != nil {
		return false
	}
	return count > 0
}

// recentPartnerWindow controls how long a partner stays excluded from
// re-selection after being used. Bumped from 24h to 72h so a small pool
// does not rotate through the same handful of mailboxes every day.
const recentPartnerWindow = 72 * time.Hour

// recentDomainWindow controls the lookback for the domain-distribution
// histogram. A week is long enough to smooth out daily randomness while
// still reflecting current behaviour.
const recentDomainWindow = 7 * 24 * time.Hour

// smallPoolWarnThreshold defines the participant count below which we log
// a warning. Tiny pools force partner reuse and create obvious patterns
// that mailbox providers can cluster on.
const smallPoolWarnThreshold = 8

// partnerDiversityWindow / partnerMaxSharedWindow set the explicit
// partner-diversity target: within partnerDiversityWindow, a sender should not
// send to the same partner more than partnerMaxSharedWindow times. Over-used
// partners are demoted to the fallback tier so warmup traffic spreads across
// many partners rather than forming a tight reciprocal pair (a closed-loop
// graph signal). This is a soft target — it only applies while enough other
// partners remain available.
const (
	partnerDiversityWindow = 7 * 24 * time.Hour
	partnerMaxSharedWindow = 3
)

func warmupPartnerRecheckTime() time.Time {
	return time.Now().Add(time.Duration(240+rand.Intn(240)) * time.Minute)
}

// selectWarmupPartner selects a warmup partner from the pool, preferring
// partners on under-represented recipient domains to avoid concentrating
// traffic on a single provider (e.g. all-Gmail warmup loops).
func (s *tasksService) selectWarmupPartner(ctx context.Context, account Email) (*Email, error) {
	poolType := s.resolveWarmupPoolType(ctx, &account)

	participantIDs, err := s.warmupRepo.GetPoolRecipientParticipants(ctx, poolType, true)
	if err != nil {
		return nil, err
	}

	if len(participantIDs) == 0 {
		return nil, fmt.Errorf("no warmup partners available")
	}

	if len(participantIDs) < smallPoolWarnThreshold {
		log.Warn().
			Int("participants", len(participantIDs)).
			Str("pool", poolType).
			Str("email_account_id", account.ID.String()).
			Msg("Warmup pool below diversity threshold; partner reuse likely")
	}

	domainsByID, err := s.warmupRepo.GetPoolParticipantDomains(ctx, poolType, true)
	if err != nil {
		// Diversity weighting is best-effort. Fall back to uniform on lookup error.
		domainsByID = nil
	}

	// Load customer routing rules (premium pool only — free pool ignores
	// rules since trial mailboxes don't need provider-shape preferences).
	var routingRules []models.WarmupRoutingRule
	var emailsByID map[uuid.UUID]string
	if poolType == "premium" && s.warmupRoutingRepo != nil && account.OrganizationID != nil {
		rules, ruleErr := s.warmupRoutingRepo.ListForOrganization(ctx, *account.OrganizationID)
		if ruleErr == nil && len(rules) > 0 {
			routingRules = rules
			if e, eErr := s.warmupRepo.GetPoolParticipantEmails(ctx, poolType, true); eErr == nil {
				emailsByID = e
			}
		}
	}

	recentPartnerSet := map[uuid.UUID]struct{}{}
	recentPartnerIDs, err := s.warmupRepo.GetRecentlyUsedPartners(ctx, account.ID, time.Now().Add(-recentPartnerWindow))
	if err == nil {
		for _, pid := range recentPartnerIDs {
			recentPartnerSet[pid] = struct{}{}
		}
	}

	todayPartnerSet := map[uuid.UUID]struct{}{}
	todayPartnerIDs, err := s.warmupRepo.GetRecentlyUsedPartners(ctx, account.ID, time.Now().Truncate(24*time.Hour))
	if err == nil {
		for _, pid := range todayPartnerIDs {
			todayPartnerSet[pid] = struct{}{}
		}
	}

	domainCounts, err := s.warmupRepo.GetRecentPartnerDomainCounts(ctx, account.ID, time.Now().Add(-recentDomainWindow))
	if err != nil {
		domainCounts = nil
	}

	// Explicit partner-diversity target: how many times each partner has been
	// used in the diversity window, so partners the sender already leans on
	// heavily get demoted out of the preferred tier.
	partnerCounts, err := s.warmupRepo.GetRecentPartnerCounts(ctx, account.ID, time.Now().Add(-partnerDiversityWindow))
	if err != nil {
		partnerCounts = nil
	}

	var availablePartners []uuid.UUID
	var fallbackPartners []uuid.UUID
	for _, id := range participantIDs {
		if id == account.ID {
			continue
		}
		if _, usedToday := todayPartnerSet[id]; usedToday {
			continue
		}
		fallbackPartners = append(fallbackPartners, id)
		_, recentlyUsed := recentPartnerSet[id]
		overUsed := partnerCounts[id] >= partnerMaxSharedWindow
		if !recentlyUsed && !overUsed {
			availablePartners = append(availablePartners, id)
		}
	}

	if len(availablePartners) == 0 && len(fallbackPartners) > 0 {
		availablePartners = fallbackPartners
	}

	if len(availablePartners) == 0 {
		return nil, fmt.Errorf("no available warmup partners")
	}

	// Pick a partner, then gate it through the SAME health re-evaluation the
	// sender passes (email_task STEP 5). The recipient-selection SQL re-admits a
	// row the instant blocked_until elapses — before the hourly sweep
	// reclassifies it — so without this gate a just-expired quarantined/blocked
	// mailbox could be chosen as a recipient with no re-qualification.
	// CanParticipate re-evaluates and forces just-unblocked mailboxes into
	// probation, matching the CLAUDE.md re-entry policy on the recipient surface.
	for attempts := 0; attempts < 5 && len(availablePartners) > 0; attempts++ {
		partnerID := pickWeightedPartner(availablePartners, domainsByID, domainCounts, routingRules, account.Email, emailsByID)

		if s.warmupHealth != nil {
			if ok, _, _ := s.warmupHealth.CanParticipate(ctx, partnerID, poolType); !ok {
				availablePartners = removePartnerID(availablePartners, partnerID)
				continue
			}
		}

		partner, err := s.emailRepo.GetByID(ctx, partnerID)
		if err != nil {
			return nil, err
		}
		return partner, nil
	}

	return nil, fmt.Errorf("no eligible warmup partners after health gate")
}

// removePartnerID returns ids without the first occurrence of target. Used to
// drop a partner that failed the health gate before re-picking.
func removePartnerID(ids []uuid.UUID, target uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}

// pickWeightedPartner picks a partner ID using a composite weight:
//   - inverse-frequency on the partner's recipient domain (diversity)
//   - customer-defined routing rule multipliers (preference)
//
// Rules are evaluated in priority order; the first matching rule for a
// (sender, recipient) pair applies its weight multiplier. A rule with
// weight=0 hard-excludes the pair. Falls back to uniform when no signals
// are available.
func pickWeightedPartner(
	candidates []uuid.UUID,
	domainsByID map[uuid.UUID]string,
	domainCounts map[string]int,
	rules []models.WarmupRoutingRule,
	senderEmail string,
	emailsByID map[uuid.UUID]string,
) uuid.UUID {
	if len(candidates) == 1 {
		return candidates[0]
	}
	if len(domainsByID) == 0 && len(rules) == 0 {
		return candidates[rand.Intn(len(candidates))]
	}

	weights := make([]float64, len(candidates))
	var total float64
	for i, id := range candidates {
		domain := domainsByID[id]
		// Diversity base weight.
		w := 1.0 / float64(1+domainCounts[domain])

		// Routing rule multiplier (premium pool only, when configured).
		if len(rules) > 0 && len(emailsByID) > 0 {
			if recipientEmail, ok := emailsByID[id]; ok {
				w *= routingMultiplier(rules, senderEmail, recipientEmail)
			}
		}

		weights[i] = w
		total += w
	}

	if total <= 0 {
		// All candidates hard-excluded by rules. Fall back to uniform so
		// the system still warms up rather than stalling on misconfigured rules.
		return candidates[rand.Intn(len(candidates))]
	}

	r := rand.Float64() * total
	var cum float64
	for i, w := range weights {
		cum += w
		if r <= cum {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

// routingMultiplier returns the weight multiplier from the first matching
// rule in priority order (lowest priority value wins). 1.0 when no rule
// matches — neutral, so unmatched pairs neither preferred nor penalized.
func routingMultiplier(rules []models.WarmupRoutingRule, senderEmail, recipientEmail string) float64 {
	for i := range rules {
		if rules[i].Matches(senderEmail, recipientEmail) {
			return rules[i].Weight
		}
	}
	return 1.0
}

func (s *tasksService) resolveWarmupPoolType(ctx context.Context, account *Email) string {
	if account == nil {
		return "premium"
	}
	if account.WarmupPoolType != "" {
		return account.WarmupPoolType
	}
	if s.featureGate != nil && account.OrganizationID != nil {
		isPaid, xerr := s.featureGate.IsPaidOrganization(ctx, *account.OrganizationID)
		if xerr == nil && !isPaid {
			return "free"
		}
	}
	return "premium"
}

func (s *tasksService) scheduleWarmupRecovery(ctx context.Context, accountID uuid.UUID, poolType string) {
	if s.warmupRepo == nil {
		return
	}

	health, err := s.warmupRepo.GetParticipantHealth(ctx, accountID, poolType)
	if err != nil || health == nil || health.BlockedUntil == nil {
		return
	}
	if !health.BlockedUntil.After(time.Now()) {
		return
	}

	if err := s.createWarmupTask(ctx, accountID, *health.BlockedUntil); err != nil {
		log.Warn().Err(err).Str("email_account_id", accountID.String()).Str("pool_type", poolType).Msg("Failed to create warmup recovery task")
	}
}

// EnsureWarmupScheduled makes sure the mailbox has a pending warmup task so
// its warmup / health-check chain is running. Safe to call repeatedly: the
// per-mailbox advisory lock in CreateWarmupTaskWithLock means a duplicate
// pending task is never created. Returns ErrWarmupNotEnabled (benign) when
// the mailbox is neither warming nor backing a live campaign.
func (s *tasksService) EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error {
	nextTime, err := s.scheduler.CalculateNextWarmupTime(ctx, accountID)
	if err != nil {
		return err
	}
	return s.createWarmupTask(ctx, accountID, nextTime)
}

// createWarmupTask creates a new warmup task in GCP Cloud Tasks
func (s *tasksService) createWarmupTask(ctx context.Context, accountID uuid.UUID, scheduleTime time.Time) error {
	// Create task in database
	newTaskID := uuid.New()
	newTask := &Task{
		ID:             newTaskID,
		TaskType:       "warmup",
		EmailAccountID: accountID,
		Status:         "pending",
		ScheduledAt:    &scheduleTime,
	}

	// Create warmup task entry
	warmupTask := &WarmupTask{
		TaskID: newTaskID,
	}

	created, err := s.taskRepo.CreateWarmupTaskWithLock(ctx, newTask, warmupTask)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	// Create GCP Cloud Task
	processTask := &proto.ProcessTask{
		TaskId: newTaskID.String(),
	}

	cloudTaskName, err := s.tasksClient.CreateTask(ctx, processTask, scheduleTime)
	if err != nil {
		return err
	}

	// Update task with cloud task name
	if err := s.taskRepo.UpdateTaskScheduledAt(ctx, newTaskID, scheduleTime, cloudTaskName); err != nil {
		return err
	}

	return nil
}

// publishWarmupEmailSentEvent publishes warmup email sent event
func (s *tasksService) publishWarmupEmailSentEvent(ctx context.Context, task *Task, account *Email, partner *Email, isReply bool) {
	if s.eventsPublisher == nil {
		return
	}

	if err := s.eventsPublisher.PublishWarmupEmailSent(ctx, task, account, partner, isReply); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("Failed to publish warmup email sent event")
	}
}

// generateWarmupSubject picks a warmup subject. To reduce content
// fingerprinting risk, ~40% of the time we synthesize a subject from
// slot templates (yielding thousands of unique strings) instead of
// returning a literal from the static set.
func generateWarmupSubject() string {
	if rand.Float64() < 0.4 {
		if s := synthesizeWarmupSubject(); s != "" {
			return s
		}
	}
	subjects := warmupSubjectLiterals()
	return subjects[rand.Intn(len(subjects))]
}

// synthesizeWarmupSubject composes a subject from slot fragments. With
// the current slot dictionaries this yields several thousand distinct
// strings, which is harder for a vendor corpus-classifier to fingerprint.
func synthesizeWarmupSubject() string {
	templates := []string{
		"{adj} {noun}",
		"{adj} {noun} {timeRef}",
		"{verb} {noun}",
		"{verb} on {noun}",
		"{question} about {noun}",
		"{timeRef} {noun}",
		"{noun} {timeRef}",
		// NB: no "Re:"/"Fwd:" template here — a fabricated reply prefix on a
		// first-touch send is a deception signal and CAN-SPAM exposure. The
		// genuine reply path adds "Re:" itself when there is a real
		// In-Reply-To header.
	}
	adj := []string{"quick", "small", "short", "tiny", "casual", "friendly", "useful", "interesting", "brief", "minor"}
	noun := []string{"check-in", "follow up", "note", "ping", "thought", "update", "idea", "heads up", "favor", "question", "nudge", "share"}
	verb := []string{"Following up", "Circling back", "Wanted your take", "Touching base", "Picking up", "Adding"}
	question := []string{"Quick question", "Curious", "Wanted your view", "Thought"}
	timeRef := []string{"this week", "before EOD", "when free", "this morning", "today", "later", "if useful"}

	tpl := templates[rand.Intn(len(templates))]
	r := strings.NewReplacer(
		"{adj}", capitalize(adj[rand.Intn(len(adj))]),
		"{noun}", noun[rand.Intn(len(noun))],
		"{verb}", verb[rand.Intn(len(verb))],
		"{question}", question[rand.Intn(len(question))],
		"{timeRef}", timeRef[rand.Intn(len(timeRef))],
	)
	return strings.TrimSpace(r.Replace(tpl))
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func warmupSubjectLiterals() []string {
	return []string{
		// Casual check-ins
		"Quick question",
		"Following up",
		"Checking in",
		"Quick update",
		"Hope your week is going well",
		"Touching base",
		"Small update",
		"Just wanted to say hi",
		"How have you been?",
		"Long time no chat",

		// Sharing / referencing
		"Thought you might find this interesting",
		"Wanted to share this",
		"Good read I came across",
		"Saw this and thought of you",
		"Worth a look",
		"You might like this",
		"Had to share this with you",
		"Reminded me of our last chat",
		"Came across something relevant",

		// Engagement / questions
		"Any thoughts on this?",
		"A quick favor",
		"When you get a chance",
		"Just a thought",
		"Would love your input",
		"Curious what you think",
		"Got a minute?",
		"Need a quick opinion",

		// Thread-like / follow-up
		"Re: our conversation",
		"One more thing",
		"Circling back",
		"Heads up",
		"Forgot to mention",
		"One thing I meant to ask",
		"Adding to what we discussed",
		"Quick follow up from earlier",

		// Seasonal / time-based
		"Happy Monday",
		"End of week thought",
		"Midweek check-in",
		"Hope the quarter is going well",
		"Starting the week strong",

		// Professional / value-add
		"Resource you might find useful",
		"Something worth bookmarking",
		"Interesting perspective on this",
		"Quick tip I picked up",
		"Thought this was insightful",
	}
}

// conversationForTheme returns a conversation matching the requested theme,
// falling back to a random conversation if the theme is empty or unknown.
// Use this when replying so the reply body stays on-topic with the original
// thread instead of jumping subjects mid-conversation.
func conversationForTheme(theme string) Conversation {
	if theme == "" {
		return randomWarmupConversation()
	}
	var matches []Conversation
	for _, c := range warmupConversations() {
		if c.Theme == theme {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return randomWarmupConversation()
	}
	return matches[rand.Intn(len(matches))]
}

func randomWarmupConversation() Conversation {
	conversations := warmupConversations()
	return conversations[rand.Intn(len(conversations))]
}

// staticConvID derives a STABLE id for a static-library conversation from its
// content. Previously each call to warmupConversations() minted fresh uuid.New()
// ids, so the conversation_id recorded on a warmup token never matched across
// sends — defeating cohort correlation and dedupe. A deterministic id makes the
// static library traceable the same way the AI bank rows are.
func staticConvID(theme, description string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmup-static:"+theme+"|"+description))
}

func warmupConversations() []Conversation {
	conversations := []Conversation{
		// Productivity & workflow
		{Theme: "productivity", Description: "I have been trying a few workflow changes and wondered what worked best for your week.", Messages: []string{"How do you structure focused work blocks?", "Do you batch similar tasks or tackle them as they come?"}},
		{Theme: "productivity", Description: "I started time-blocking my calendar this month and the results have been interesting so far.", Messages: []string{"Have you tried any time management methods that actually stuck?", "What does your typical morning routine look like?"}},
		{Theme: "automation", Description: "I automated a couple of repetitive tasks recently and it freed up more time than I expected.", Messages: []string{"Are there any repetitive tasks in your day that you have managed to streamline?"}},

		// Learning & growth
		{Theme: "learning", Description: "I came across a useful article and it got me curious about what resources you rely on lately.", Messages: []string{"Any newsletter or podcast you consistently recommend?", "What is the best thing you have learned recently?"}},
		{Theme: "learning", Description: "I have been dedicating an hour each week to learning something new and it has been surprisingly rewarding.", Messages: []string{"How do you make time for professional development?"}},
		{Theme: "courses", Description: "I just wrapped up an online course that was really practical and well-structured.", Messages: []string{"Have you taken any courses lately that were worth the investment?"}},

		// Collaboration & teams
		{Theme: "collaboration", Description: "I was thinking about how teams keep communication clear when work gets busy.", Messages: []string{"What has helped your team keep projects moving smoothly?", "How do you handle async communication across time zones?"}},
		{Theme: "meetings", Description: "We cut our meeting load in half last month and the team seems more productive overall.", Messages: []string{"How do you decide which meetings are actually necessary?", "Have you found a good balance between sync and async?"}},

		// Industry & trends
		{Theme: "industry", Description: "I noticed a shift in how people are approaching this topic and wanted to get your take.", Messages: []string{"Have you seen any changes in how your industry handles this?", "What trends are you paying attention to right now?"}},
		{Theme: "market", Description: "The market has been moving fast lately and I have been trying to figure out what matters most.", Messages: []string{"How are you adapting your approach given recent changes?"}},

		// Tools & technology
		{Theme: "tools", Description: "I recently switched up a few tools in my daily workflow and the difference has been noticeable.", Messages: []string{"What tools have made the biggest impact for you this year?", "Have you found a good alternative for that?"}},
		{Theme: "software", Description: "I have been testing a new project management setup and wondering if I am overcomplicating things.", Messages: []string{"What is your go-to for keeping projects organized?", "Do you prefer simple tools or full-featured platforms?"}},

		// Networking & catch-ups
		{Theme: "networking", Description: "It has been a while since we last connected and I wanted to see how things are going on your end.", Messages: []string{"Any new projects or goals you are excited about?", "What has been keeping you busy lately?"}},
		{Theme: "catchup", Description: "I was cleaning up my contacts list and realized we have not caught up in ages.", Messages: []string{"How has your year been going so far?", "Anything interesting happening on your side?"}},
		{Theme: "introduction", Description: "I met someone recently who reminded me of the work you do and thought you two should connect.", Messages: []string{"Would you be open to a quick intro?"}},

		// Feedback & advice
		{Theme: "feedback", Description: "I have been working on something and would really value a second opinion before moving forward.", Messages: []string{"Would you mind taking a quick look when you have a moment?", "I would appreciate your honest feedback on this."}},
		{Theme: "advice", Description: "I am facing a decision and I think your perspective could really help me think it through.", Messages: []string{"Have you dealt with anything similar before?", "What would you do in this situation?"}},

		// Planning & strategy
		{Theme: "planning", Description: "I am mapping out priorities for the next quarter and trying to stay realistic about what is achievable.", Messages: []string{"How do you decide what to focus on when everything feels urgent?", "What is your process for setting quarterly goals?"}},
		{Theme: "strategy", Description: "I have been rethinking how we allocate resources across projects and it is harder than it sounds.", Messages: []string{"How do you balance long-term bets with short-term wins?"}},

		// Reading & content
		{Theme: "reading", Description: "I just finished a great book that changed how I think about a few things at work.", Messages: []string{"Read anything good lately that stuck with you?", "Any books you keep recommending to people?"}},
		{Theme: "content", Description: "I have been curating a reading list and looking for suggestions outside my usual topics.", Messages: []string{"What is the most surprising thing you have read recently?"}},

		// Travel & experiences
		{Theme: "travel", Description: "I am starting to plan a trip and looking for recommendations from people who have been there.", Messages: []string{"Any travel tips or favorite destinations you would suggest?", "Where was the last place you traveled that exceeded expectations?"}},
		{Theme: "food", Description: "I tried a new restaurant last week that was genuinely impressive and thought you might enjoy it too.", Messages: []string{"Have you discovered any great spots lately?"}},

		// Wellness & balance
		{Theme: "wellness", Description: "I have been trying to be more intentional about work-life balance and curious how others handle it.", Messages: []string{"What do you do to recharge after a busy stretch?", "Have you found any habits that help you stay consistent?"}},
		{Theme: "fitness", Description: "I recently picked up a new workout routine and it has been making a real difference in my energy levels.", Messages: []string{"Do you have a go-to way to stay active during busy weeks?"}},

		// Events & community
		{Theme: "events", Description: "I saw a conference coming up that might be relevant and wanted to flag it for you.", Messages: []string{"Are you attending any events or meetups soon?", "What was the last event you went to that was actually worthwhile?"}},
		{Theme: "community", Description: "I have been getting more involved in a professional community and it has been a great source of ideas.", Messages: []string{"Are you part of any groups or communities you find valuable?"}},

		// Hiring & careers
		{Theme: "hiring", Description: "We have been expanding the team and I have been learning a lot about what makes a strong hire.", Messages: []string{"What do you look for when bringing someone new on board?"}},
		{Theme: "career", Description: "I have been reflecting on where I want to be in the next few years and it is a useful exercise.", Messages: []string{"How do you think about career growth without burning out?"}},

		// Gratitude & appreciation
		{Theme: "gratitude", Description: "I was thinking about the people who have been helpful to me this year and you came to mind.", Messages: []string{"Just wanted to say thanks for being a great connection.", "Appreciate you always being willing to share your perspective."}},
	}

	// Assign stable, content-derived ids so a given static conversation has the
	// same id on every send (see staticConvID).
	for i := range conversations {
		conversations[i].ID = staticConvID(conversations[i].Theme, conversations[i].Description)
	}

	return conversations
}
