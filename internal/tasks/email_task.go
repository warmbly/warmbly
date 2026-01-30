package tasks

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func (s *tasksService) HandleEmailTask(task *proto.ProcessTask) *errx.Error {
	ctx := context.Background()

	// STEP 1: Parse task ID
	taskID, err := uuid.Parse(task.TaskId)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.BadRequest, "invalid task ID")
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

	// STEP 3.5: Check if organization can use warmup (only paid orgs)
	if s.featureGate != nil && account.OrganizationID != nil {
		canWarmup, _ := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
		if !canWarmup {
			// Organization cannot use warmup - skip this task
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_no_warmup_access")
			return nil
		}
	}

	// STEP 4: Mark task as active (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "active"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 5: Determine if this should be a reply or new email
	replyRate := account.WarmupReplyRate
	shouldReply := rand.Float64()*100 < float64(replyRate)

	var subject, emailBody string
	var inReplyTo string

	if shouldReply {
		// TODO: Find warmup email to reply to from Cassandra
		// For now, just send new email
		shouldReply = false
	}

	// STEP 6: Generate warmup email content
	if !shouldReply {
		// Get random conversation from database
		// TODO: Implement conversation retrieval
		// For now, generate simple warmup email
		subject = generateWarmupSubject()
		emailBody = generateWarmupBody(account.Name)
	}

	// STEP 7: Select warmup partner from pool
	partner, err := s.selectWarmupPartner(ctx, *account)
	if err != nil {
		s.taskRepo.RecordTaskFailure(ctx, taskID, "No warmup partner", err.Error())
		return nil
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
		// Non-fatal: log but continue sending
		fmt.Printf("Failed to create warmup token: %v\n", err)
	} else {
		warmupTokenStr = warmupToken.String()
	}

	// STEP 10: Send warmup email to worker via Kafka
	emailMsg := EmailMessage{
		From:        account.Email,
		To:          []string{partner.Email},
		Subject:     subject,
		BodyHTML:    "",          // Warmup emails are plaintext only
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
		return nil
	}

	// STEP 11: Update task record
	taskRecord.MessageID = messageID
	taskRecord.Status = "completed"

	// STEP 12: Update warmup statistics
	if err := s.warmupRepo.IncrementDailyCount(ctx, account.ID, time.Now()); err != nil {
		fmt.Printf("Failed to increment daily count: %v\n", err)
	}

	// STEP 13: Mark task completed (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 14: Publish events
	s.publishWarmupEmailSentEvent(ctx, taskRecord, account)

	// STEP 15: Calculate next warmup time and create new task
	nextTime, err := s.scheduler.CalculateNextWarmupTime(ctx, account.ID)
	if err != nil {
		fmt.Printf("Failed to calculate next warmup time: %v\n", err)
		return nil
	}

	if err := s.createWarmupTask(ctx, account.ID, nextTime); err != nil {
		fmt.Printf("Failed to create next warmup task: %v\n", err)
	}

	return nil
}

// selectWarmupPartner selects a warmup partner from the pool
func (s *tasksService) selectWarmupPartner(ctx context.Context, account Email) (*Email, error) {
	// Determine pool type based on user subscription
	// Only paid users can reach here (free users blocked in HandleEmailTask)
	// All paid users use premium pool
	poolType := "premium"

	// Check if organization is paid to determine pool type
	if s.featureGate != nil && account.OrganizationID != nil {
		isPaid, _ := s.featureGate.IsPaidOrganization(ctx, *account.OrganizationID)
		if !isPaid {
			poolType = "free"
		}
	}

	// Get all participants in the pool
	participantIDs, err := s.warmupRepo.GetPoolParticipants(ctx, poolType, true)
	if err != nil {
		return nil, err
	}

	if len(participantIDs) == 0 {
		return nil, fmt.Errorf("no warmup partners available")
	}

	// Filter out sender's own accounts and recently used partners
	var availablePartners []uuid.UUID
	for _, id := range participantIDs {
		if id != account.ID {
			// TODO: Check if not recently used
			availablePartners = append(availablePartners, id)
		}
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
func (s *tasksService) publishWarmupEmailSentEvent(ctx context.Context, task *Task, account *Email) {
	// TODO: Implement Kafka event publishing
	fmt.Printf("Warmup email sent: task=%s, account=%s\n", task.ID, account.ID)
}

// generateWarmupSubject generates a random warmup email subject
func generateWarmupSubject() string {
	subjects := []string{
		"Quick question",
		"Following up",
		"Checking in",
		"Quick update",
		"Thought you might find this interesting",
	}
	return subjects[rand.Intn(len(subjects))]
}

// generateWarmupBody generates a simple warmup email body
func generateWarmupBody(senderName string) string {
	bodies := []string{
		fmt.Sprintf("Hi,\n\nI wanted to reach out and see how things are going.\n\nBest regards,\n%s", senderName),
		fmt.Sprintf("Hello,\n\nJust checking in to see if you had any questions.\n\nThanks,\n%s", senderName),
		fmt.Sprintf("Hi there,\n\nI hope this email finds you well.\n\nBest,\n%s", senderName),
	}
	return bodies[rand.Intn(len(bodies))]
}

