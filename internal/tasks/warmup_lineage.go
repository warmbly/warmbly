package tasks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"github.com/warmbly/warmbly/internal/repository"
)

func resolveStaticConversation(id uuid.UUID) (Conversation, bool) {
	if c, ok := ResolveDiagnosticConversation(id); ok {
		return c, true
	}
	for _, c := range warmupConversations() {
		if c.ID == id {
			return c, true
		}
	}
	return Conversation{}, false
}

func lintDiagnosticContent(subject, body string, reply bool, account Email) error {
	identity := account.Name
	if identity == "" {
		identity = account.Email
	}
	canonical := strings.TrimSuffix(body, "\n\nSimulated diagnostic.\n"+identity)
	return lintWarmupContent(subject, canonical, reply)
}

func (s *tasksService) exactReplyContent(ctx context.Context, c *repository.WarmupReplyCandidate, account Email) (warmupContent, string, []string, error) {
	if c == nil || c.ConversationID == nil || c.MessageID == "" || c.Subject == "" || c.ConversationTurn+1 >= c.MaxTurns || c.RenderingVersion != generation.CanonicalRenderingVersion {
		return warmupContent{}, "", nil, errors.New("diagnostic parent unavailable")
	}
	conv, err := s.diagnosticSource(ctx, c)
	if err != nil {
		return warmupContent{}, "", nil, err
	}
	body, ok := GenerateConversationReplyEmail(conv, account, c.ConversationTurn+1)
	if !ok {
		return warmupContent{}, "", nil, errors.New("diagnostic source exhausted")
	}
	refs, err := mailhdr.ReferenceIDs(append(append([]string{}, c.References...), c.MessageID))
	if err != nil {
		return warmupContent{}, "", nil, err
	}
	return warmupContent{subject: c.Subject, body: body, theme: c.ConversationTheme, contentSource: c.ContentSource, conversationID: c.ConversationID, scenarioVersion: c.ScenarioVersion, renderingVersion: c.RenderingVersion, maxTurns: c.MaxTurns}, c.MessageID, refs, nil
}

func (s *tasksService) diagnosticSource(ctx context.Context, c *repository.WarmupReplyCandidate) (Conversation, error) {
	var conv Conversation
	if c == nil || c.ConversationID == nil || c.RenderingVersion != generation.CanonicalRenderingVersion {
		return conv, errors.New("diagnostic source unavailable")
	}
	if c.ContentSource == models.WarmupContentSourceAI {
		if s.warmupContentRepo == nil {
			return conv, errors.New("diagnostic source unavailable")
		}
		cached, err := s.warmupContentRepo.GetConversation(ctx, *c.ConversationID)
		if err != nil {
			return conv, err
		}
		if cached == nil || cached.Status != models.WarmupConversationActive || !cached.LintPassed || !cached.ReplyEligible || cached.ScenarioVersion == nil || cached.RenderingVersion == nil || *cached.ScenarioVersion != c.ScenarioVersion || *cached.RenderingVersion != c.RenderingVersion {
			return conv, errors.New("diagnostic source retired or unverified")
		}
		conv = Conversation{ID: cached.ID, Version: *cached.ScenarioVersion, Subject: cached.Subject, Theme: cached.Theme, Description: cached.Description, Messages: cached.Messages}
	} else if c.ContentSource == models.WarmupContentSourceStatic {
		var ok bool
		conv, ok = resolveStaticConversation(*c.ConversationID)
		if !ok {
			return conv, errors.New("diagnostic source missing")
		}
	} else {
		return conv, errors.New("diagnostic source unknown")
	}
	if conv.Subject != c.Subject || conv.Version != c.ScenarioVersion {
		return conv, errors.New("diagnostic source changed")
	}
	return conv, nil
}

func (s *tasksService) closeWarmupThread(ctx context.Context, taskID, accountID uuid.UUID) *errx.Error {
	if err := s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled"); err != nil {
		return errx.InternalError()
	}
	_ = s.createWarmupTask(ctx, accountID, time.Now().Add(4*time.Hour))
	return nil
}

func (s *tasksService) sendAdmission(ctx context.Context, account *Email) error {
	recovery, ok := s.taskRepo.(repository.SendResultRecovery)
	if !ok || account.OrganizationID == nil {
		return errors.New("send authority unavailable")
	}
	admission, err := recovery.GetSendAdmission(ctx, *account.OrganizationID, account.ID, models.InboxProvider(account.Provider), time.Now())
	if err != nil {
		return err
	}
	if admission == nil || !admission.Allowed {
		return errors.New("mailbox send held")
	}
	return nil
}

func (s *tasksService) validateWarmupPair(ctx context.Context, sender, recipient *Email, pool string, reply bool) error {
	if !sender.TestSendingAllowed() || !recipient.TestReceivingAllowed() {
		return errors.New("diagnostic participation unavailable")
	}
	if reply && !sender.IsWarmingActive() {
		return errors.New("reply consent unavailable")
	}
	allowed, err := s.warmupRepo.IsPoolEligible(ctx, sender.ID, pool, true)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("sender pool authority unavailable")
	}
	allowed, err = s.warmupRepo.IsPoolEligible(ctx, recipient.ID, pool, false)
	if err != nil {
		return err
	}
	if !allowed && s.replyMayCrossTiers(ctx, sender, pool) {
		allowed, err = s.warmupRepo.IsPoolEligible(ctx, recipient.ID, otherPoolType(pool), false)
	}
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("recipient pool authority unavailable")
	}
	return nil
}

func (s *tasksService) ValidateWarmupExecution(ctx context.Context, taskID uuid.UUID) error {
	task, err := s.taskRepo.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return errors.New("warmup task unavailable")
	}
	gate, ok := s.scheduler.(interface {
		WarmupExecutionNotBefore(context.Context, uuid.UUID, uuid.UUID, time.Time) (time.Time, error)
	})
	if !ok {
		return errors.New("warmup execution policy unavailable")
	}
	now := time.Now()
	at, err := gate.WarmupExecutionNotBefore(ctx, task.EmailAccountID, taskID, now)
	if err != nil {
		return err
	}
	if at.After(now) {
		return errors.New("warmup execution is outside current policy")
	}
	warmup, err := s.taskRepo.GetWarmupTask(ctx, taskID)
	if err != nil {
		return err
	}
	if warmup == nil {
		return errors.New("warmup lineage unavailable")
	}
	lineage, ok := s.taskRepo.(repository.WarmupLineageRepository)
	if !ok {
		return errors.New("warmup lineage authority unavailable")
	}
	recipientID, current, err := lineage.WarmupExecutionContext(ctx, taskID)
	if err != nil {
		return err
	}
	account, xerr := s.emailRepo.GetByID(ctx, task.EmailAccountID)
	if xerr != nil {
		return xerr
	}
	if account == nil {
		return errors.New("warmup mailbox unavailable")
	}
	if !account.TestSendingAllowed() {
		return errors.New("diagnostic sending stopped")
	}
	source, err := s.diagnosticSource(ctx, current)
	if err != nil {
		return err
	}
	if current.ConversationTurn < 0 || current.ConversationTurn >= current.MaxTurns {
		return errors.New("diagnostic source exhausted")
	}
	if current.ConversationTurn == 0 {
		if GenerateConversationOpeningEmail(source, *account) == "" {
			return errors.New("diagnostic opening unavailable")
		}
	} else if _, ok := GenerateConversationReplyEmail(source, *account, current.ConversationTurn); !ok {
		return errors.New("diagnostic turn unavailable")
	}
	reader, ok := s.warmupRepo.(interface {
		WarmupExecutionCandidates(context.Context, string, uuid.UUID, uuid.UUID) ([]models.WarmupPartnerCandidate, error)
	})
	if !ok {
		return errors.New("warmup recipient capacity unavailable")
	}
	pool := s.resolveWarmupPoolType(ctx, account)
	candidates, err := reader.WarmupExecutionCandidates(ctx, pool, account.ID, taskID)
	if err != nil {
		return err
	}
	allowed := false
	for _, candidate := range candidates {
		if candidate.ID == recipientID {
			allowed = true
			break
		}
	}
	if !allowed {
		return errors.New("warmup recipient no longer eligible")
	}
	if pool == "premium" && s.warmupRoutingRepo != nil && account.OrganizationID != nil {
		rules, err := s.warmupRoutingRepo.ListForOrganization(ctx, *account.OrganizationID)
		if err != nil {
			return err
		}
		recipient, xerr := s.emailRepo.GetByID(ctx, recipientID)
		if xerr != nil {
			return xerr
		}
		if recipient == nil || routingMultiplier(rules, account.Email, recipient.Email) <= 0 {
			return errors.New("warmup recipient excluded")
		}
	}
	if warmup.ParentTaskID != nil {
		lineage, ok := s.taskRepo.(repository.WarmupLineageRepository)
		if !ok {
			return errors.New("warmup lineage authority unavailable")
		}
		parent, err := lineage.ExactWarmupParent(ctx, taskID)
		if err != nil {
			return err
		}
		account, xerr := s.emailRepo.GetByID(ctx, task.EmailAccountID)
		if xerr != nil {
			return xerr
		}
		if account == nil {
			return errors.New("warmup mailbox unavailable")
		}
		if _, _, _, err = s.exactReplyContent(ctx, parent, *account); err != nil {
			return err
		}
	}
	return nil
}
