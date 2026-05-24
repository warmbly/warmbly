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

	// STEP 3: Load email account
	account, xerr := s.emailRepo.GetByID(ctx, taskRecord.EmailAccountID)
	if xerr != nil {
		return xerr
	}
	if account == nil {
		return errx.ErrNotFound
	}
	if account.Status != "active" || account.Warmup == nil {
		if s.warmupHealth != nil {
			_ = s.warmupHealth.RemovePoolMembership(ctx, account.ID, s.resolveWarmupPoolType(ctx, account))
		}
		_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
		executionStatus = "completed"
		return nil
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
		if err := s.warmupHealth.EnsurePoolMembership(ctx, account.ID, poolType); err != nil {
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
		s.taskRepo.RecordTaskFailure(ctx, taskID, "No warmup partner", err.Error())
		if s.advanced != nil {
			_ = s.advanced.CaptureTaskDeadLetter(ctx, taskID, "warmup", map[string]interface{}{
				"reason": "no_warmup_partner",
			}, err.Error(), 1)
			_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "dead_lettered")
		}
		return nil
	}

	// STEP 6: Determine if this should be a reply or a new warmup email
	replyRate := account.WarmupReplyRate
	shouldReply := rand.Float64()*100 < float64(replyRate)
	var subject, emailBody string
	var inReplyTo string

	if shouldReply {
		candidate, replyErr := s.warmupRepo.GetLatestReplyCandidate(ctx, partner.ID, account.ID)
		if replyErr == nil && candidate != nil && candidate.MessageID != "" {
			inReplyTo = candidate.MessageID
			subject = strings.TrimSpace(candidate.Subject)
			if subject == "" {
				subject = generateWarmupSubject()
			}
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
			emailBody = GenerateConversationEmail(randomWarmupConversation(), *account, true)
		} else {
			shouldReply = false
		}
	}

	// STEP 7: Build a new warmup message when not replying
	if !shouldReply {
		conversation := randomWarmupConversation()
		subject = generateWarmupSubject()
		emailBody = GenerateConversationEmail(conversation, *account, false)
	}

	// STEP 8: Encrypt content
	userUUID, err := uuid.Parse(account.UserID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	cipher, err := s.cipherService.Cipher(ctx, userUUID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	_, err = cipher.Encrypt(ctx, subject)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	_, err = cipher.Encrypt(ctx, emailBody)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 9: Generate Message-ID
	messageID := generateMessageID(account.Email)

	// STEP 9.5: Generate warmup verification token
	var warmupTokenStr string
	warmupToken := uuid.New()
	tokenRecord := &models.WarmupToken{
		Token:              warmupToken,
		TaskID:             taskID,
		SenderAccountID:    account.ID,
		RecipientAccountID: partner.ID,
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
		UserID:      userUUID,
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

// selectWarmupPartner selects a warmup partner from the pool
func (s *tasksService) selectWarmupPartner(ctx context.Context, account Email) (*Email, error) {
	poolType := s.resolveWarmupPoolType(ctx, &account)

	// Get all participants in the pool
	participantIDs, err := s.warmupRepo.GetPoolParticipants(ctx, poolType, true)
	if err != nil {
		return nil, err
	}

	if len(participantIDs) == 0 {
		return nil, fmt.Errorf("no warmup partners available")
	}

	// Filter out sender's own account and recently used partners.
	recentPartnerSet := map[uuid.UUID]struct{}{}
	recentPartnerIDs, err := s.warmupRepo.GetRecentlyUsedPartners(ctx, account.ID, time.Now().Add(-24*time.Hour))
	if err == nil {
		for _, pid := range recentPartnerIDs {
			recentPartnerSet[pid] = struct{}{}
		}
	}

	var availablePartners []uuid.UUID
	var fallbackPartners []uuid.UUID
	for _, id := range participantIDs {
		if id != account.ID {
			fallbackPartners = append(fallbackPartners, id)
			if _, recentlyUsed := recentPartnerSet[id]; !recentlyUsed {
				availablePartners = append(availablePartners, id)
			}
		}
	}

	if len(availablePartners) == 0 && len(fallbackPartners) > 0 {
		availablePartners = fallbackPartners
	}

	if len(availablePartners) == 0 {
		return nil, fmt.Errorf("no available warmup partners")
	}

	// Select random partner
	partnerID := availablePartners[rand.Intn(len(availablePartners))]

	// Load partner account
	partner, err := s.emailRepo.GetByID(ctx, partnerID)
	if err != nil {
		return nil, err
	}

	return partner, nil
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

	if err := s.taskRepo.CreateTask(ctx, newTask); err != nil {
		return err
	}

	// Create warmup task entry
	warmupTask := &WarmupTask{
		TaskID: newTaskID,
	}

	if err := s.taskRepo.CreateWarmupTask(ctx, warmupTask); err != nil {
		return err
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

// generateWarmupSubject generates a random warmup email subject
func generateWarmupSubject() string {
	subjects := []string{
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
	return subjects[rand.Intn(len(subjects))]
}

func randomWarmupConversation() Conversation {
	conversations := []Conversation{
		// Productivity & workflow
		{ID: uuid.New(), Theme: "productivity", Description: "I have been trying a few workflow changes and wondered what worked best for your week.", Messages: []string{"How do you structure focused work blocks?", "Do you batch similar tasks or tackle them as they come?"}},
		{ID: uuid.New(), Theme: "productivity", Description: "I started time-blocking my calendar this month and the results have been interesting so far.", Messages: []string{"Have you tried any time management methods that actually stuck?", "What does your typical morning routine look like?"}},
		{ID: uuid.New(), Theme: "automation", Description: "I automated a couple of repetitive tasks recently and it freed up more time than I expected.", Messages: []string{"Are there any repetitive tasks in your day that you have managed to streamline?"}},

		// Learning & growth
		{ID: uuid.New(), Theme: "learning", Description: "I came across a useful article and it got me curious about what resources you rely on lately.", Messages: []string{"Any newsletter or podcast you consistently recommend?", "What is the best thing you have learned recently?"}},
		{ID: uuid.New(), Theme: "learning", Description: "I have been dedicating an hour each week to learning something new and it has been surprisingly rewarding.", Messages: []string{"How do you make time for professional development?"}},
		{ID: uuid.New(), Theme: "courses", Description: "I just wrapped up an online course that was really practical and well-structured.", Messages: []string{"Have you taken any courses lately that were worth the investment?"}},

		// Collaboration & teams
		{ID: uuid.New(), Theme: "collaboration", Description: "I was thinking about how teams keep communication clear when work gets busy.", Messages: []string{"What has helped your team keep projects moving smoothly?", "How do you handle async communication across time zones?"}},
		{ID: uuid.New(), Theme: "meetings", Description: "We cut our meeting load in half last month and the team seems more productive overall.", Messages: []string{"How do you decide which meetings are actually necessary?", "Have you found a good balance between sync and async?"}},

		// Industry & trends
		{ID: uuid.New(), Theme: "industry", Description: "I noticed a shift in how people are approaching this topic and wanted to get your take.", Messages: []string{"Have you seen any changes in how your industry handles this?", "What trends are you paying attention to right now?"}},
		{ID: uuid.New(), Theme: "market", Description: "The market has been moving fast lately and I have been trying to figure out what matters most.", Messages: []string{"How are you adapting your approach given recent changes?"}},

		// Tools & technology
		{ID: uuid.New(), Theme: "tools", Description: "I recently switched up a few tools in my daily workflow and the difference has been noticeable.", Messages: []string{"What tools have made the biggest impact for you this year?", "Have you found a good alternative for that?"}},
		{ID: uuid.New(), Theme: "software", Description: "I have been testing a new project management setup and wondering if I am overcomplicating things.", Messages: []string{"What is your go-to for keeping projects organized?", "Do you prefer simple tools or full-featured platforms?"}},

		// Networking & catch-ups
		{ID: uuid.New(), Theme: "networking", Description: "It has been a while since we last connected and I wanted to see how things are going on your end.", Messages: []string{"Any new projects or goals you are excited about?", "What has been keeping you busy lately?"}},
		{ID: uuid.New(), Theme: "catchup", Description: "I was cleaning up my contacts list and realized we have not caught up in ages.", Messages: []string{"How has your year been going so far?", "Anything interesting happening on your side?"}},
		{ID: uuid.New(), Theme: "introduction", Description: "I met someone recently who reminded me of the work you do and thought you two should connect.", Messages: []string{"Would you be open to a quick intro?"}},

		// Feedback & advice
		{ID: uuid.New(), Theme: "feedback", Description: "I have been working on something and would really value a second opinion before moving forward.", Messages: []string{"Would you mind taking a quick look when you have a moment?", "I would appreciate your honest feedback on this."}},
		{ID: uuid.New(), Theme: "advice", Description: "I am facing a decision and I think your perspective could really help me think it through.", Messages: []string{"Have you dealt with anything similar before?", "What would you do in this situation?"}},

		// Planning & strategy
		{ID: uuid.New(), Theme: "planning", Description: "I am mapping out priorities for the next quarter and trying to stay realistic about what is achievable.", Messages: []string{"How do you decide what to focus on when everything feels urgent?", "What is your process for setting quarterly goals?"}},
		{ID: uuid.New(), Theme: "strategy", Description: "I have been rethinking how we allocate resources across projects and it is harder than it sounds.", Messages: []string{"How do you balance long-term bets with short-term wins?"}},

		// Reading & content
		{ID: uuid.New(), Theme: "reading", Description: "I just finished a great book that changed how I think about a few things at work.", Messages: []string{"Read anything good lately that stuck with you?", "Any books you keep recommending to people?"}},
		{ID: uuid.New(), Theme: "content", Description: "I have been curating a reading list and looking for suggestions outside my usual topics.", Messages: []string{"What is the most surprising thing you have read recently?"}},

		// Travel & experiences
		{ID: uuid.New(), Theme: "travel", Description: "I am starting to plan a trip and looking for recommendations from people who have been there.", Messages: []string{"Any travel tips or favorite destinations you would suggest?", "Where was the last place you traveled that exceeded expectations?"}},
		{ID: uuid.New(), Theme: "food", Description: "I tried a new restaurant last week that was genuinely impressive and thought you might enjoy it too.", Messages: []string{"Have you discovered any great spots lately?"}},

		// Wellness & balance
		{ID: uuid.New(), Theme: "wellness", Description: "I have been trying to be more intentional about work-life balance and curious how others handle it.", Messages: []string{"What do you do to recharge after a busy stretch?", "Have you found any habits that help you stay consistent?"}},
		{ID: uuid.New(), Theme: "fitness", Description: "I recently picked up a new workout routine and it has been making a real difference in my energy levels.", Messages: []string{"Do you have a go-to way to stay active during busy weeks?"}},

		// Events & community
		{ID: uuid.New(), Theme: "events", Description: "I saw a conference coming up that might be relevant and wanted to flag it for you.", Messages: []string{"Are you attending any events or meetups soon?", "What was the last event you went to that was actually worthwhile?"}},
		{ID: uuid.New(), Theme: "community", Description: "I have been getting more involved in a professional community and it has been a great source of ideas.", Messages: []string{"Are you part of any groups or communities you find valuable?"}},

		// Hiring & careers
		{ID: uuid.New(), Theme: "hiring", Description: "We have been expanding the team and I have been learning a lot about what makes a strong hire.", Messages: []string{"What do you look for when bringing someone new on board?"}},
		{ID: uuid.New(), Theme: "career", Description: "I have been reflecting on where I want to be in the next few years and it is a useful exercise.", Messages: []string{"How do you think about career growth without burning out?"}},

		// Gratitude & appreciation
		{ID: uuid.New(), Theme: "gratitude", Description: "I was thinking about the people who have been helpful to me this year and you came to mind.", Messages: []string{"Just wanted to say thanks for being a great connection.", "Appreciate you always being willing to share your perspective."}},
	}

	return conversations[rand.Intn(len(conversations))]
}
