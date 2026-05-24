// Package dangerzone implements the "danger zone" pattern for high-stakes
// destructive actions: scheduling a deletion that runs only after a long
// grace period, with cancellation and email warnings.
//
// The pattern (used by GitLab, GCP, Atlassian, etc.) trades the
// convenience of an immediate delete for the safety of a recovery window.
// Users overwhelmingly prefer "I clicked the wrong button and have a
// week to undo it" over "I clicked the wrong button and lost everything."
package dangerzone

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service is the public API for the danger zone subsystem.
type Service interface {
	// Organization
	ScheduleOrganizationDeletion(ctx context.Context, orgID, requesterUserID uuid.UUID, req *models.ScheduleDeletionRequest) (*models.ScheduledDeletion, *errx.Error)
	CancelOrganizationDeletion(ctx context.Context, orgID, requesterUserID uuid.UUID, req *models.CancelDeletionRequest) *errx.Error
	GetOrganizationStatus(ctx context.Context, orgID uuid.UUID) (*models.DangerZoneStatus, *errx.Error)

	// User account
	ScheduleUserDeletion(ctx context.Context, userID uuid.UUID, req *models.ScheduleDeletionRequest) (*models.ScheduledDeletion, *errx.Error)
	CancelUserDeletion(ctx context.Context, userID uuid.UUID, req *models.CancelDeletionRequest) *errx.Error
	GetUserStatus(ctx context.Context, userID uuid.UUID) (*models.DangerZoneStatus, *errx.Error)

	// Background-job entry points
	ExecuteDuePendingDeletions(ctx context.Context) (executed int, failed int, err error)
	DispatchReminders(ctx context.Context) error
}

// service is the default Service implementation.
type service struct {
	repo     repository.DangerZoneRepository
	orgRepo  repository.OrganizationRepository
	userRepo repository.UserRepository

	notifier notify.EmailNotificationService

	// frontendBaseURL is used when building cancellation links in emails.
	// Falls back to "https://app.warmbly.com" if empty.
	frontendBaseURL string
}

// NewService constructs the default Service implementation.
func NewService(
	repo repository.DangerZoneRepository,
	orgRepo repository.OrganizationRepository,
	userRepo repository.UserRepository,
	notifier notify.EmailNotificationService,
	frontendBaseURL string,
) Service {
	if frontendBaseURL == "" {
		frontendBaseURL = "https://app.warmbly.com"
	}
	return &service{
		repo:            repo,
		orgRepo:         orgRepo,
		userRepo:        userRepo,
		notifier:        notifier,
		frontendBaseURL: frontendBaseURL,
	}
}

// ---------- Organization ----------

func (s *service) ScheduleOrganizationDeletion(ctx context.Context, orgID, requesterUserID uuid.UUID, req *models.ScheduleDeletionRequest) (*models.ScheduledDeletion, *errx.Error) {
	if req == nil {
		return nil, errx.New(errx.BadRequest, "request body required")
	}

	org, err := s.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load organization")
	}
	if org == nil {
		return nil, errx.ErrNotFound
	}

	if org.OwnerUserID != requesterUserID {
		return nil, errx.New(errx.Forbidden, "only the organization owner can schedule deletion")
	}

	if org.DeletionScheduledFor != nil {
		return nil, errx.New(errx.Conflict, "organization is already scheduled for deletion")
	}

	if !confirmationMatches(req.Confirmation, org.Name) {
		return nil, errx.New(errx.BadRequest, "confirmation phrase does not match organization name")
	}

	now := time.Now()
	d := &models.ScheduledDeletion{
		ID:                uuid.New(),
		ResourceType:      models.DeletionResourceOrganization,
		ResourceID:        org.ID,
		OrganizationID:    &org.ID,
		RequestedByUserID: requesterUserID,
		Reason:            nilIfEmpty(req.Reason),
		ScheduledAt:       now,
		ExecuteAfter:      now.Add(time.Duration(models.OrganizationDeletionGraceDays) * 24 * time.Hour),
		GraceDays:         models.OrganizationDeletionGraceDays,
		Status:            models.DeletionStatusPending,
	}

	if err := s.repo.CreatePending(ctx, d); err != nil {
		if errors.Is(err, repository.ErrPendingDeletionExists) {
			return nil, errx.New(errx.Conflict, "organization is already scheduled for deletion")
		}
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to schedule organization deletion")
	}

	go s.sendOrgScheduledEmail(context.Background(), org, d)
	_ = s.repo.SetNotifBit(ctx, d.ID, models.DeletionNotifInitial)

	return d, nil
}

func (s *service) CancelOrganizationDeletion(ctx context.Context, orgID, requesterUserID uuid.UUID, req *models.CancelDeletionRequest) *errx.Error {
	org, err := s.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to load organization")
	}
	if org == nil {
		return errx.ErrNotFound
	}
	if org.OwnerUserID != requesterUserID {
		return errx.New(errx.Forbidden, "only the organization owner can cancel deletion")
	}

	d, err := s.repo.GetActive(ctx, models.DeletionResourceOrganization, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to load pending deletion")
	}
	if d == nil {
		return errx.New(errx.BadRequest, "organization is not pending deletion")
	}

	reason := ""
	if req != nil {
		reason = req.Reason
	}

	if err := s.repo.Cancel(ctx, d.ID, requesterUserID, reason); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to cancel deletion")
	}

	go s.sendOrgCancelledEmail(context.Background(), org, d)
	return nil
}

func (s *service) GetOrganizationStatus(ctx context.Context, orgID uuid.UUID) (*models.DangerZoneStatus, *errx.Error) {
	org, err := s.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load organization")
	}
	if org == nil {
		return nil, errx.ErrNotFound
	}

	status := &models.DangerZoneStatus{
		ResourceType:     models.DeletionResourceOrganization,
		ResourceID:       org.ID,
		ResourceName:     org.Name,
		ConfirmationHint: org.Name,
		GraceDays:        models.OrganizationDeletionGraceDays,
	}

	d, err := s.repo.GetActive(ctx, models.DeletionResourceOrganization, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load pending deletion")
	}
	status.PendingDeletion = d
	return status, nil
}

// ---------- User account ----------

func (s *service) ScheduleUserDeletion(ctx context.Context, userID uuid.UUID, req *models.ScheduleDeletionRequest) (*models.ScheduledDeletion, *errx.Error) {
	if req == nil {
		return nil, errx.New(errx.BadRequest, "request body required")
	}

	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load user")
	}
	if user == nil {
		return nil, errx.ErrNotFound
	}

	if user.DeletionScheduledFor != nil {
		return nil, errx.New(errx.Conflict, "account is already scheduled for deletion")
	}

	if !confirmationMatches(req.Confirmation, user.Email) {
		return nil, errx.New(errx.BadRequest, "confirmation phrase does not match your email")
	}

	now := time.Now()
	d := &models.ScheduledDeletion{
		ID:                uuid.New(),
		ResourceType:      models.DeletionResourceUser,
		ResourceID:        userID,
		RequestedByUserID: userID,
		Reason:            nilIfEmpty(req.Reason),
		ScheduledAt:       now,
		ExecuteAfter:      now.Add(time.Duration(models.UserDeletionGraceDays) * 24 * time.Hour),
		GraceDays:         models.UserDeletionGraceDays,
		Status:            models.DeletionStatusPending,
	}

	if err := s.repo.CreatePending(ctx, d); err != nil {
		if errors.Is(err, repository.ErrPendingDeletionExists) {
			return nil, errx.New(errx.Conflict, "account is already scheduled for deletion")
		}
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to schedule account deletion")
	}

	go s.sendUserScheduledEmail(context.Background(), user, d)
	_ = s.repo.SetNotifBit(ctx, d.ID, models.DeletionNotifInitial)

	return d, nil
}

func (s *service) CancelUserDeletion(ctx context.Context, userID uuid.UUID, req *models.CancelDeletionRequest) *errx.Error {
	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to load user")
	}
	if user == nil {
		return errx.ErrNotFound
	}

	d, err := s.repo.GetActive(ctx, models.DeletionResourceUser, userID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to load pending deletion")
	}
	if d == nil {
		return errx.New(errx.BadRequest, "account is not pending deletion")
	}

	reason := ""
	if req != nil {
		reason = req.Reason
	}

	if err := s.repo.Cancel(ctx, d.ID, userID, reason); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to cancel deletion")
	}

	go s.sendUserCancelledEmail(context.Background(), user, d)
	return nil
}

func (s *service) GetUserStatus(ctx context.Context, userID uuid.UUID) (*models.DangerZoneStatus, *errx.Error) {
	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load user")
	}
	if user == nil {
		return nil, errx.ErrNotFound
	}

	status := &models.DangerZoneStatus{
		ResourceType:     models.DeletionResourceUser,
		ResourceID:       userID,
		ResourceName:     displayName(user),
		ConfirmationHint: user.Email,
		GraceDays:        models.UserDeletionGraceDays,
	}

	d, err := s.repo.GetActive(ctx, models.DeletionResourceUser, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to load pending deletion")
	}
	status.PendingDeletion = d
	return status, nil
}

// ---------- Background-job entry points ----------

// executeBatchSize bounds how many deletions one tick will execute.
// Keeps a single tick's wall-clock time bounded if many resources come due.
const executeBatchSize = 50

// ExecuteDuePendingDeletions runs every due deletion. Returns counts of
// successes and failures. The caller (the scheduler) is expected to call
// this on a tick (~every hour is fine: the grace window is in days).
func (s *service) ExecuteDuePendingDeletions(ctx context.Context) (int, int, error) {
	due, err := s.repo.ListDue(ctx, time.Now(), executeBatchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("list due deletions: %w", err)
	}

	var executed, failed int
	for i := range due {
		d := due[i]

		// MarkExecuting also serves as the lock: if two workers race, only
		// one transitions pending -> executing.
		claimed, err := s.repo.MarkExecuting(ctx, d.ID)
		if err != nil {
			sentry.CaptureException(err)
			failed++
			continue
		}
		if !claimed {
			continue
		}

		if err := s.runHardDelete(ctx, &d); err != nil {
			sentry.CaptureException(err)
			_ = s.repo.MarkFailed(ctx, d.ID, err.Error())
			failed++
			continue
		}

		if err := s.repo.MarkCompleted(ctx, d.ID); err != nil {
			sentry.CaptureException(err)
			failed++
			continue
		}

		go s.sendCompletionEmail(context.Background(), &d)
		_ = s.repo.SetNotifBit(ctx, d.ID, models.DeletionNotifCompletion)
		executed++
	}

	return executed, failed, nil
}

// runHardDelete performs the actual destructive DB operation for a single
// scheduled deletion. Errors propagate so the row can be marked failed.
func (s *service) runHardDelete(ctx context.Context, d *models.ScheduledDeletion) error {
	switch d.ResourceType {
	case models.DeletionResourceOrganization:
		return s.repo.HardDeleteOrganization(ctx, d.ResourceID)
	case models.DeletionResourceUser:
		return s.repo.HardDeleteUser(ctx, d.ResourceID)
	default:
		return fmt.Errorf("unsupported resource type: %s", d.ResourceType)
	}
}

// DispatchReminders sends the 7-day and 24-hour warning emails. Each bit
// is set atomically so a reminder is never sent twice.
func (s *service) DispatchReminders(ctx context.Context) error {
	reminderTiers := []struct {
		within time.Duration
		bit    int
	}{
		{within: 7 * 24 * time.Hour, bit: models.DeletionNotif7Day},
		{within: 24 * time.Hour, bit: models.DeletionNotif24Hour},
	}

	for _, t := range reminderTiers {
		batch, err := s.repo.ListPendingForReminders(ctx, t.within, t.bit, 100)
		if err != nil {
			return fmt.Errorf("list reminders for bit %d: %w", t.bit, err)
		}
		for i := range batch {
			d := batch[i]
			s.sendReminderEmail(ctx, &d, t.bit)
			if err := s.repo.SetNotifBit(ctx, d.ID, t.bit); err != nil {
				sentry.CaptureException(err)
			}
		}
	}
	return nil
}

// ---------- Email senders ----------

func (s *service) sendOrgScheduledEmail(ctx context.Context, org *models.Organization, d *models.ScheduledDeletion) {
	if s.notifier == nil {
		return
	}
	recipients := s.orgRecipients(ctx, org)
	if len(recipients) == 0 {
		return
	}
	subject := fmt.Sprintf("%s scheduled for deletion", org.Name)
	body := orgScheduledHTML(org, d, s.frontendBaseURL)
	s.sendToEach(ctx, recipients, subject, body)
}

func (s *service) sendOrgCancelledEmail(ctx context.Context, org *models.Organization, d *models.ScheduledDeletion) {
	if s.notifier == nil {
		return
	}
	recipients := s.orgRecipients(ctx, org)
	if len(recipients) == 0 {
		return
	}
	subject := fmt.Sprintf("Deletion cancelled for %s", org.Name)
	body := orgCancelledHTML(org, d)
	s.sendToEach(ctx, recipients, subject, body)
}

func (s *service) sendUserScheduledEmail(ctx context.Context, user *models.User, d *models.ScheduledDeletion) {
	if s.notifier == nil {
		return
	}
	subject := "Your Warmbly account is scheduled for deletion"
	body := userScheduledHTML(user, d, s.frontendBaseURL)
	if err := s.notifier.Send(ctx, []string{user.Email}, nil, nil, subject, body); err != nil {
		sentry.CaptureException(err)
	}
}

func (s *service) sendUserCancelledEmail(ctx context.Context, user *models.User, d *models.ScheduledDeletion) {
	if s.notifier == nil {
		return
	}
	subject := "Your account deletion was cancelled"
	body := userCancelledHTML(user, d)
	if err := s.notifier.Send(ctx, []string{user.Email}, nil, nil, subject, body); err != nil {
		sentry.CaptureException(err)
	}
}

func (s *service) sendReminderEmail(ctx context.Context, d *models.ScheduledDeletion, bit int) {
	if s.notifier == nil {
		return
	}

	recipients, subject, body := s.buildReminder(ctx, d, bit)
	if len(recipients) == 0 {
		return
	}
	s.sendToEach(ctx, recipients, subject, body)
}

func (s *service) buildReminder(ctx context.Context, d *models.ScheduledDeletion, bit int) (recipients []string, subject, body string) {
	var resourceName string
	switch d.ResourceType {
	case models.DeletionResourceOrganization:
		org, _ := s.orgRepo.GetByID(ctx, d.ResourceID)
		if org == nil {
			return nil, "", ""
		}
		recipients = s.orgRecipients(ctx, org)
		resourceName = org.Name
	case models.DeletionResourceUser:
		user, _ := s.userRepo.GetUser(ctx, d.ResourceID)
		if user == nil {
			return nil, "", ""
		}
		recipients = []string{user.Email}
		resourceName = displayName(user)
	default:
		return nil, "", ""
	}

	if len(recipients) == 0 {
		return nil, "", ""
	}

	switch bit {
	case models.DeletionNotif7Day:
		subject = fmt.Sprintf("Reminder: %s will be deleted in 7 days", resourceName)
	case models.DeletionNotif24Hour:
		subject = fmt.Sprintf("Final reminder: %s will be deleted in 24 hours", resourceName)
	default:
		subject = fmt.Sprintf("Deletion reminder for %s", resourceName)
	}

	body = reminderHTML(resourceName, d, s.frontendBaseURL)
	return recipients, subject, body
}

func (s *service) sendCompletionEmail(ctx context.Context, d *models.ScheduledDeletion) {
	if s.notifier == nil {
		return
	}

	// The resource is gone, so we look up the requester instead.
	requester, _ := s.userRepo.GetUser(ctx, d.RequestedByUserID)
	if requester == nil {
		return
	}

	var subject string
	switch d.ResourceType {
	case models.DeletionResourceOrganization:
		subject = "Your organization has been deleted"
	case models.DeletionResourceUser:
		// User row is gone; we can't email them. Skip.
		return
	default:
		return
	}

	body := completionHTML(d)
	if err := s.notifier.Send(ctx, []string{requester.Email}, nil, nil, subject, body); err != nil {
		sentry.CaptureException(err)
	}
}

// ---------- Helpers ----------

func confirmationMatches(input, expected string) bool {
	return strings.TrimSpace(strings.ToLower(input)) == strings.TrimSpace(strings.ToLower(expected))
}

func nilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func displayName(u *models.User) string {
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
	if name != "" {
		return name
	}
	return u.Email
}

// orgRecipients returns every member email for an org, owner first.
// Owner is always included even if GetMembers somehow fails to load
// them (defensive — the owner is the only person who can actually
// cancel, so they MUST get the email).
func (s *service) orgRecipients(ctx context.Context, org *models.Organization) []string {
	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)

	if owner, _ := s.userRepo.GetUser(ctx, org.OwnerUserID); owner != nil && owner.Email != "" {
		key := strings.ToLower(owner.Email)
		seen[key] = struct{}{}
		out = append(out, owner.Email)
	}

	members, err := s.orgRepo.GetMembers(ctx, org.ID)
	if err != nil {
		sentry.CaptureException(err)
		return out
	}
	for i := range members {
		u := members[i].User
		if u == nil || u.Email == "" {
			continue
		}
		key := strings.ToLower(u.Email)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, u.Email)
	}
	return out
}

// sendToEach mails one-to-one rather than To/Cc/Bcc-fanning. Keeps
// every recipient unaware of the others' addresses, and makes per-
// person bounces/spam reports cleanly attributable.
func (s *service) sendToEach(ctx context.Context, recipients []string, subject, body string) {
	for _, to := range recipients {
		if err := s.notifier.Send(ctx, []string{to}, nil, nil, subject, body); err != nil {
			sentry.CaptureException(err)
		}
	}
}
