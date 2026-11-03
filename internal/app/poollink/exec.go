package poollink

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	ErrSelfExecutionNotGranted   = errx.NewWithIdentifier(errx.Forbidden, "pool_link_self_execution_not_granted", "Keeping mailbox credentials on your instance is not enabled for this Cloud workspace. Ask Warmbly support to enable it.")
	ErrSelfExecutionUnavailable  = errx.NewWithIdentifier(errx.Conflict, "pool_link_self_execution_unavailable", "This Cloud deployment cannot run warmup through your instance.")
	ErrExecutorInvalid           = errx.NewWithIdentifier(errx.BadRequest, "pool_link_executor", "Executor must be cloud or instance.")
	ErrExecutorCredential        = errx.NewWithIdentifier(errx.BadRequest, "pool_link_executor_credential", "A mailbox your instance runs is enrolled without a credential.")
	ErrExecutorConflict          = errx.NewWithIdentifier(errx.Conflict, "pool_link_executor_conflict", "This mailbox's credential stays on your instance, so Cloud does not accept one for it. Update the instance, or switch the mailbox to Cloud explicitly.")
	ErrExecutorManaged           = errx.NewWithIdentifier(errx.Unprocessable, "pool_link_executor_managed", "A mailbox Cloud connected through its own sign-in cannot be run by your instance.")
	ErrMailboxNotSelfExecuted    = errx.NewWithIdentifier(errx.Conflict, "pool_link_mailbox_not_self_executed", "Cloud runs this mailbox's warmup, so it takes no reports from the instance.")
	ErrCommandNotFound           = errx.NewWithIdentifier(errx.NotFound, "pool_link_command_not_found", "Command not found.")
	ErrCommandNotLeased          = errx.NewWithIdentifier(errx.Conflict, "pool_link_command_not_leased", "Lease the command before answering it.")
	ErrCommandAnswered           = errx.NewWithIdentifier(errx.Conflict, "pool_link_command_answered", "This command already has a different answer.")
	ErrCommandResultInvalid      = errx.NewWithIdentifier(errx.Unprocessable, "pool_link_command_result_invalid", "The result does not fit the command.")
	ErrArrivalInvalid            = errx.NewWithIdentifier(errx.BadRequest, "pool_link_arrival_invalid", "A warmup report needs a message handle, Message-ID, sender and received time.")
	ErrArrivalPending            = errx.NewWithIdentifier(errx.ServiceUnavailable, "pool_link_arrival_pending", "Cloud has not recorded this send yet. Report it again shortly.")
	ErrArrivalNotFound           = errx.NewWithIdentifier(errx.NotFound, "pool_link_arrival_not_found", "No accepted warmup message has that handle.")
	ErrObservationInvalid        = errx.NewWithIdentifier(errx.BadRequest, "pool_link_observation_invalid", "Observation kind must be removed, moved_to_spam or owner_activity.")
	errSelfExecutionDependencies = errors.New("pool link: self-execution is not wired")
)

// GrantSource is the operator's permission for credential-free Cloud warmup.
type GrantSource interface {
	GetSelfExecutionGrant(ctx context.Context, orgID uuid.UUID) (*models.SelfExecutionGrant, error)
}

// OutboundValidator is the same execution check a worker's send start passes.
type OutboundValidator interface {
	ValidateOutboundExecution(ctx context.Context, taskID uuid.UUID) error
}

// OperatorNotifier raises an operator alert.
type OperatorNotifier interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

// CredentialRestorer writes a credential back for a mailbox returning to Cloud.
type CredentialRestorer interface {
	RestoreMailboxCredential(ctx context.Context, id uuid.UUID, oauth *models.PoolLinkOAuthCredential, creds *models.SmtpImap) error
}

// SelfExecutionDeps are what instance-executed warmup needs beyond the base service.
type SelfExecutionDeps struct {
	Grants   GrantSource
	Cipher   cipher.CipherService
	Jobs     JobPublisher
	Workers  events.Publisher
	Outbound OutboundValidator
	Notifier OperatorNotifier
	Restorer CredentialRestorer
	Clock    func() time.Time
}

type selfExecution struct {
	SelfExecutionDeps
	repo repository.PoolLinkExecRepository
}

// SelfExecutionService is the Cloud side of mailboxes a linked instance executes.
type SelfExecutionService interface {
	LeaseCommands(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkLeaseRequest) (*models.PoolLinkLeaseResponse, *errx.Error)
	AnswerCommand(ctx context.Context, inst *models.PoolLinkInstance, commandID uuid.UUID, r models.PoolLinkCommandResult) *errx.Error
	ReportArrival(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, a models.PoolLinkWarmupArrival) (*models.PoolLinkArrivalVerdict, *errx.Error)
	ReportObservation(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, o models.PoolLinkWarmupObservation) *errx.Error
	ListSelfExecutionMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkSelfExecutionMailbox, *errx.Error)
	SelfExecutionReady(ctx context.Context, accountID uuid.UUID) (executed, ready bool, err error)
	SweepSelfExecution(ctx context.Context) error
	AuditReporterTrust(ctx context.Context) error
}

// WireSelfExecution enables instance-executed mailboxes on this Cloud.
func (s *service) WireSelfExecution(d SelfExecutionDeps) {
	x, ok := s.repo.(repository.PoolLinkExecRepository)
	if !ok || d.Grants == nil || d.Cipher == nil || d.Jobs == nil {
		return
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	s.exec = &selfExecution{SelfExecutionDeps: d, repo: x}
}

// WireOutboundValidator attaches the send execution check, built after the service.
func (s *service) WireOutboundValidator(v OutboundValidator) {
	if s.exec != nil {
		s.exec.Outbound = v
	}
}

func (s *service) now() time.Time {
	if s.exec != nil {
		return s.exec.Clock()
	}
	return time.Now()
}

// selfExecutionGrant is the workspace's grant when it is in force, else nil.
func (s *service) selfExecutionGrant(ctx context.Context, orgID uuid.UUID) (*models.SelfExecutionGrant, error) {
	if s.exec == nil {
		return nil, nil
	}
	g, err := s.exec.Grants.GetSelfExecutionGrant(ctx, orgID)
	if err != nil || !g.Active(s.now()) {
		return nil, err
	}
	return g, nil
}

// selfExecutionInfo is the advertised protocol and the workspace's grant.
func (s *service) selfExecutionInfo(ctx context.Context, orgID uuid.UUID) *models.PoolLinkSelfExecution {
	if s.exec == nil {
		return nil
	}
	info := &models.PoolLinkSelfExecution{Protocol: models.PoolLinkSelfExecutionProtocol}
	g, err := s.exec.Grants.GetSelfExecutionGrant(ctx, orgID)
	if err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("pool link: self-execution grant unavailable")
		return info
	}
	if g.Active(s.now()) {
		info.Granted, info.ExpiresAt = true, g.ExpiresAt
	}
	return info
}

func (s *service) SelfExecutionReady(ctx context.Context, accountID uuid.UUID) (bool, bool, error) {
	if s.exec == nil {
		return false, false, nil
	}
	m, err := s.exec.repo.InstanceExecuted(ctx, accountID)
	if err != nil || m == nil {
		return false, false, err
	}
	live, err := s.exec.repo.SelfExecutionLive(ctx, accountID)
	return true, live, err
}

// enrollExecutor routes an enrollment by who will run the mailbox.
func (s *service) enrollExecutor(ctx context.Context, inst *models.PoolLinkInstance, existing *models.PoolLinkMailbox, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error, bool) {
	if !req.Executor.Valid() {
		return nil, ErrExecutorInvalid, true
	}
	hasCredential := req.OAuth != nil || req.SMTPIMAP != nil
	if existing == nil {
		if req.Executor != models.PoolLinkExecutorInstance {
			return nil, nil, false
		}
		out, xerr := s.enrollInstanceExecuted(ctx, inst, req)
		return out, xerr, true
	}
	current := existing.Executor.OrCloud()
	switch {
	case req.Executor == "" && current == models.PoolLinkExecutorInstance && hasCredential:
		// An empty executor keeps the current one, so a credential is refused rather than read as a switch.
		return nil, ErrExecutorConflict, true
	case req.Executor == "" || req.Executor == current:
		if current == models.PoolLinkExecutorInstance {
			if hasCredential {
				return nil, ErrExecutorCredential, true
			}
			out, xerr := s.PatchMailbox(ctx, inst, req.RemoteID, models.PoolLinkMailboxPatch{Warmup: &req.Warmup})
			return out, xerr, true
		}
		return nil, nil, false
	case req.Executor == models.PoolLinkExecutorInstance:
		if hasCredential {
			return nil, ErrExecutorCredential, true
		}
		out, xerr := s.convertToInstance(ctx, inst, existing, req)
		return out, xerr, true
	default:
		out, xerr := s.convertToCloud(ctx, inst, existing, req)
		return out, xerr, true
	}
}

func (s *service) requireGrant(ctx context.Context, orgID uuid.UUID) *errx.Error {
	if s.exec == nil {
		return ErrSelfExecutionUnavailable
	}
	g, err := s.selfExecutionGrant(ctx, orgID)
	if err != nil {
		return errx.InternalError()
	}
	if g == nil {
		return ErrSelfExecutionNotGranted
	}
	return nil
}

func (s *service) enrollInstanceExecuted(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if req.OAuth != nil || req.SMTPIMAP != nil {
		return nil, ErrExecutorCredential
	}
	switch req.Provider {
	case models.InboxProviderGoogle, models.InboxProviderOutlook, models.InboxProviderSMTPIMAP:
	default:
		return nil, ErrBadCredential
	}
	if xerr := s.requireGrant(ctx, inst.OrganizationID); xerr != nil {
		return nil, xerr
	}
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.enrollInstanceExecuted(ctx, inst, req)
			return xerr
		})
		return out, xerr
	}
	plan, xerr := s.Plan(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	if !plan.WarmupEntitled {
		return nil, ErrWarmupNotEntitled
	}
	if xerr := admitNewMailbox(plan); xerr != nil {
		return nil, xerr
	}
	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr != nil {
		return nil, xerr
	}
	address := strings.TrimSpace(req.Email)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = address
	}
	m, err := s.exec.repo.EnrollInstanceExecuted(ctx, repository.InstanceExecutedEnrollment{
		InstanceID: inst.ID, RemoteID: req.RemoteID, OrganizationID: inst.OrganizationID, UserID: userID,
		Email: address, Name: name, Provider: req.Provider, MailHost: instanceMailHost(req.Provider, address),
	})
	if err != nil {
		var xe *errx.Error
		if errors.As(err, &xe) {
			if errors.Is(xe, errx.ErrEmailOnboardAlreadyExists) {
				return nil, ErrMailboxConflict
			}
			return nil, xe
		}
		return nil, enrollError(err)
	}
	s.activateEnrolled(ctx, inst, userID, m.EmailAccountID, req)
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

func instanceMailHost(provider models.InboxProvider, address string) string {
	domain := ""
	if at := strings.LastIndex(address, "@"); at >= 0 {
		domain = strings.ToLower(address[at+1:])
	}
	switch provider {
	case models.InboxProviderGoogle:
		return string(mailhost.Refine(mailhost.GoogleWorkspace, domain))
	case models.InboxProviderOutlook:
		return string(mailhost.Refine(mailhost.Microsoft365, domain))
	}
	return ""
}

// convertToInstance hands a Cloud-run mailbox to its instance and deletes Cloud's credential.
func (s *service) convertToInstance(ctx context.Context, inst *models.PoolLinkInstance, existing *models.PoolLinkMailbox, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if existing.Managed {
		return nil, ErrExecutorManaged
	}
	if xerr := s.requireGrant(ctx, inst.OrganizationID); xerr != nil {
		return nil, xerr
	}
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.convertToInstance(ctx, inst, existing, req)
			return xerr
		})
		return out, xerr
	}
	worker, err := s.exec.repo.ConvertToInstance(ctx, inst.ID, req.RemoteID)
	if errors.Is(err, repository.ErrPoolLinkNotSelfExecuted) {
		return nil, ErrExecutorManaged
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	if worker != nil && s.exec.Workers != nil {
		owner, _ := s.ownerUserID(ctx, inst)
		if err := s.exec.Workers.PublishRemoveEmail(ctx, *worker, &models.RemoveWorkerEmail{UserID: owner, EmailID: existing.EmailAccountID.String()}); err != nil {
			log.Warn().Err(err).Str("account_id", existing.EmailAccountID.String()).Msg("pool link: previous worker not told to drop a mailbox handed to its instance")
		}
	}
	if userID, xerr := s.ownerUserID(ctx, inst); xerr == nil {
		s.applyWarmupSettings(ctx, inst.OrganizationID, userID, existing.EmailAccountID, req.Warmup)
	}
	if s.scheduler != nil {
		_ = s.scheduler.EnsureWarmupScheduled(ctx, existing.EmailAccountID)
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

// convertToCloud returns an instance-run mailbox to Cloud with the credential the instance now shares.
func (s *service) convertToCloud(ctx context.Context, inst *models.PoolLinkInstance, existing *models.PoolLinkMailbox, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	acc, xerr := s.emails.GetByID(ctx, existing.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	switch models.InboxProvider(acc.Provider) {
	case models.InboxProviderGoogle, models.InboxProviderOutlook:
		if req.OAuth == nil || req.OAuth.RefreshToken == "" {
			return nil, ErrBadCredential
		}
	case models.InboxProviderSMTPIMAP:
		if req.SMTPIMAP == nil || req.SMTPIMAP.SMTP == nil || req.SMTPIMAP.IMAP == nil {
			return nil, ErrBadCredential
		}
		if xerr := cloudReachable(req.SMTPIMAP); xerr != nil {
			return nil, xerr
		}
	default:
		return nil, ErrBadCredential
	}
	if s.exec == nil || s.exec.Restorer == nil {
		return nil, ErrSelfExecutionUnavailable
	}
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.convertToCloud(ctx, inst, existing, req)
			return xerr
		})
		return out, xerr
	}
	if err := s.exec.Restorer.RestoreMailboxCredential(ctx, existing.EmailAccountID, req.OAuth, req.SMTPIMAP); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.exec.repo.ConvertToCloud(ctx, inst.ID, req.RemoteID); err != nil {
		return nil, errx.InternalError()
	}
	if userID, xerr := s.ownerUserID(ctx, inst); xerr == nil {
		s.applyWarmupSettings(ctx, inst.OrganizationID, userID, existing.EmailAccountID, req.Warmup)
	}
	if err := s.emailSvc.LoadAccountOntoWorker(ctx, existing.EmailAccountID); err != nil {
		log.Warn().Err(err).Str("account_id", existing.EmailAccountID.String()).Msg("pool link: worker load failed after return to Cloud; reconciler will retry")
	}
	if s.scheduler != nil {
		_ = s.scheduler.EnsureWarmupScheduled(ctx, existing.EmailAccountID)
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

func (s *service) LeaseCommands(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkLeaseRequest) (*models.PoolLinkLeaseResponse, *errx.Error) {
	if s.exec == nil {
		return nil, ErrSelfExecutionUnavailable
	}
	now := s.now()
	out := &models.PoolLinkLeaseResponse{Commands: []models.PoolLinkCommand{}, LeasedUntil: now}
	g, err := s.selfExecutionGrant(ctx, inst.OrganizationID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if g == nil {
		// Without a grant nothing is handed out and the executor is not counted live.
		return out, nil
	}
	limit := req.Limit
	if limit <= 0 || limit > config.PoolLinkLeaseLimitMax {
		limit = config.PoolLinkLeaseLimitMax
	}
	out.LeasedUntil = now.Add(config.PoolLinkCommandLeaseSeconds * time.Second)
	stored, err := s.exec.repo.LeaseCommands(ctx, inst.ID, limit, out.LeasedUntil)
	if err != nil {
		return nil, errx.InternalError()
	}
	for i := range stored {
		if cmd := s.leased(ctx, &stored[i]); cmd != nil {
			out.Commands = append(out.Commands, *cmd)
		}
	}
	return out, nil
}

// leased turns a leased row into the command handed out, starting a send on the way; nil drops it.
func (s *service) leased(ctx context.Context, c *repository.StoredPoolLinkCommand) *models.PoolLinkCommand {
	x := s.exec
	env, err := openEnvelope(ctx, x.Cipher, c.OrganizationID, c.Payload)
	if err != nil || (c.Kind == models.PoolLinkCommandSend) != (env.Send != nil) || (c.Kind == models.PoolLinkCommandAction && env.Action == nil) {
		log.Warn().Err(err).Str("command_id", c.ID.String()).Msg("pool link: unreadable command withdrawn")
		s.withdraw(ctx, c)
		return nil
	}
	if c.Kind == models.PoolLinkCommandSend {
		if x.Outbound != nil {
			if verr := x.Outbound.ValidateOutboundExecution(ctx, *c.TaskID); verr != nil {
				if transientValidation(verr) {
					s.releaseLease(ctx, c)
					return nil
				}
				s.withdraw(ctx, c)
				return nil
			}
		}
		started, err := x.repo.BeginSelfExecutedSend(ctx, *c.TaskID, c.EmailAccountID, *c.DispatchNonce)
		if err != nil {
			s.releaseLease(ctx, c)
			return nil
		}
		if !started {
			if cerr := x.repo.CancelCommand(ctx, c.ID); cerr != nil {
				log.Warn().Err(cerr).Str("command_id", c.ID.String()).Msg("pool link: refused send not withdrawn")
			}
			return nil
		}
	}
	return &models.PoolLinkCommand{ID: c.ID, RemoteID: c.RemoteID, Kind: c.Kind, Send: env.Send, Action: env.Action, NotAfter: c.NotAfter, CreatedAt: c.CreatedAt}
}

// releaseLease puts a command back in the queue when it could not be checked this time.
func (s *service) releaseLease(ctx context.Context, c *repository.StoredPoolLinkCommand) {
	if err := s.exec.repo.ReleaseCommandLease(ctx, c.ID); err != nil {
		log.Warn().Err(err).Str("command_id", c.ID.String()).Msg("pool link: command lease not released")
	}
}

// withdraw cancels a command that will not be handed out, releasing a send's reservation with it.
func (s *service) withdraw(ctx context.Context, c *repository.StoredPoolLinkCommand) {
	if c.Kind == models.PoolLinkCommandSend && c.TaskID != nil && c.DispatchNonce != nil {
		if err := s.exec.repo.CancelSelfExecutedSend(ctx, *c.TaskID, c.EmailAccountID, *c.DispatchNonce); err != nil {
			log.Warn().Err(err).Str("command_id", c.ID.String()).Msg("pool link: withdrawn send not released")
		}
	}
	if err := s.exec.repo.CancelCommand(ctx, c.ID); err != nil {
		log.Warn().Err(err).Str("command_id", c.ID.String()).Msg("pool link: command not withdrawn")
	}
}

func (s *service) AnswerCommand(ctx context.Context, inst *models.PoolLinkInstance, commandID uuid.UUID, r models.PoolLinkCommandResult) *errx.Error {
	if s.exec == nil {
		return ErrSelfExecutionUnavailable
	}
	now := s.now()
	var refusal *errx.Error
	err := s.exec.repo.AnswerCommand(ctx, inst.ID, commandID, func(c *repository.StoredPoolLinkCommand) (*repository.PoolLinkCommandAnswer, error) {
		var env *commandEnvelope
		if c.Kind == models.PoolLinkCommandAction {
			opened, err := openEnvelope(ctx, s.exec.Cipher, c.OrganizationID, c.Payload)
			if err != nil {
				return nil, err
			}
			env = opened
		}
		answer, xerr := decideAnswer(c, env, r, now)
		if xerr != nil {
			refusal = xerr
			return nil, nil
		}
		if answer != nil && answer.events != nil {
			evs := answer.events
			answer.Publish = func(ctx context.Context) error { return s.publishJobs(ctx, c.EmailAccountID, evs) }
		}
		if answer == nil {
			return nil, nil
		}
		return &answer.PoolLinkCommandAnswer, nil
	})
	if errors.Is(err, repository.ErrPoolLinkCommandNotFound) {
		return ErrCommandNotFound
	}
	if err != nil {
		log.Warn().Err(err).Str("command_id", commandID.String()).Msg("pool link: command answer not stored")
		return errx.InternalError()
	}
	return refusal
}

func (s *service) publishJobs(ctx context.Context, account uuid.UUID, evs []*models.JobEvent) error {
	for _, ev := range evs {
		if err := s.exec.Jobs.PublishJob(ctx, account.String(), ev); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) ReportArrival(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, a models.PoolLinkWarmupArrival) (*models.PoolLinkArrivalVerdict, *errx.Error) {
	if s.exec == nil {
		return nil, ErrSelfExecutionUnavailable
	}
	m, xerr := s.executedMailbox(ctx, inst, remoteID)
	if xerr != nil {
		return nil, xerr
	}
	a.MessageHandle, a.MessageID, a.From = strings.TrimSpace(a.MessageHandle), strings.TrimSpace(a.MessageID), strings.TrimSpace(a.From)
	if a.MessageHandle == "" || len(a.MessageHandle) > 512 || a.MessageID == "" || !validMessageID(a.MessageID) || a.From == "" || a.ReceivedAt.IsZero() {
		return nil, ErrArrivalInvalid
	}
	now := s.now()
	proof, verdict, xerr := s.arrivalProof(ctx, m, a)
	if xerr != nil || verdict != nil {
		return verdict, xerr
	}
	verdict, pending := checkArrival(m.EmailAccountID, a, *proof, now)
	if pending {
		return nil, ErrArrivalPending
	}
	if verdict.Verdict != models.PoolLinkArrivalAccepted {
		return verdict, nil
	}
	internal := arrivalInternalID(m.EmailAccountID, a.MessageHandle)
	sender := proof.Token.SenderAccountID
	recorded, err := s.exec.repo.RecordArrival(ctx, repository.PoolLinkArrival{
		EmailAccountID: m.EmailAccountID, MessageHandle: a.MessageHandle, MessageID: a.MessageID, InternalID: internal,
		SenderAccountID: &sender, LandedSpam: a.LandedSpam || strings.EqualFold(a.Folder, models.FolderSpam), ReceivedAt: a.ReceivedAt,
	})
	if err != nil {
		return nil, errx.InternalError()
	}
	if !recorded {
		return &models.PoolLinkArrivalVerdict{Verdict: models.PoolLinkArrivalDuplicate}, nil
	}
	ev := arrivalEvent(m, a, internal, proof.TokenPresented, proof.Token.Token, now)
	if err := s.exec.Jobs.PublishJob(ctx, m.EmailAccountID.String(), ev); err != nil {
		if derr := s.exec.repo.DeleteArrival(ctx, m.EmailAccountID, a.MessageHandle); derr != nil {
			log.Warn().Err(derr).Str("account_id", m.EmailAccountID.String()).Msg("pool link: unpublished arrival not withdrawn")
		}
		return nil, ErrArrivalPending
	}
	return verdict, nil
}

// arrivalProof finds the send an arrival claims to be, or answers it outright.
func (s *service) arrivalProof(ctx context.Context, m *repository.PoolLinkExecMailbox, a models.PoolLinkWarmupArrival) (*arrivalEvidence, *models.PoolLinkArrivalVerdict, *errx.Error) {
	if s.warmup == nil {
		return nil, nil, ErrSelfExecutionUnavailable
	}
	var token *models.WarmupToken
	presented := false
	if raw := strings.TrimSpace(a.WarmupToken); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, &models.PoolLinkArrivalVerdict{Verdict: models.PoolLinkArrivalRejected, Reason: ReasonTokenUnknown}, nil
		}
		if token, err = s.warmup.FindWarmupToken(ctx, id); err != nil {
			return nil, nil, errx.InternalError()
		}
		presented = token != nil
	}
	if token == nil {
		found, err := s.warmup.FindDeliveredWarmupToken(ctx, m.EmailAccountID, senderAddress(a.From), a.MessageID, a.Subject)
		if errors.Is(err, repository.ErrWarmupDeliveryPending) {
			return nil, nil, ErrArrivalPending
		}
		if err != nil {
			return nil, nil, errx.InternalError()
		}
		if found == nil {
			if strings.TrimSpace(a.WarmupToken) != "" {
				return nil, &models.PoolLinkArrivalVerdict{Verdict: models.PoolLinkArrivalRejected, Reason: ReasonTokenUnknown}, nil
			}
			return nil, &models.PoolLinkArrivalVerdict{Verdict: models.PoolLinkArrivalNotWarmup}, nil
		}
		token = found
	}
	sender, xerr := s.emails.GetByID(ctx, token.SenderAccountID)
	if xerr != nil && xerr != errx.ErrNotFound {
		return nil, nil, ErrArrivalPending
	}
	send, err := s.exec.repo.WarmupTaskSend(ctx, token.TaskID, token.SenderAccountID)
	if err != nil {
		return nil, nil, ErrArrivalPending
	}
	ev := arrivalEvidenceFor(token, presented, sender, send)
	return &ev, nil, nil
}

func (s *service) ReportObservation(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, o models.PoolLinkWarmupObservation) *errx.Error {
	if s.exec == nil {
		return ErrSelfExecutionUnavailable
	}
	m, xerr := s.executedMailbox(ctx, inst, remoteID)
	if xerr != nil {
		return xerr
	}
	switch o.Kind {
	case models.PoolLinkObservationRemoved, models.PoolLinkObservationMovedToSpam, models.PoolLinkObservationOwnerActive:
	default:
		return ErrObservationInvalid
	}
	var arrival *repository.PoolLinkArrival
	var err error
	if handle := strings.TrimSpace(o.MessageHandle); handle != "" {
		arrival, err = s.exec.repo.GetArrival(ctx, m.EmailAccountID, handle)
	}
	// A duplicate report was stored under its first handle, so the Message-ID finds it.
	if id := strings.TrimSpace(o.MessageID); err == nil && arrival == nil && id != "" {
		arrival, err = s.exec.repo.ArrivalForMessage(ctx, m.EmailAccountID, id)
	}
	if err != nil {
		return errx.InternalError()
	}
	if arrival == nil {
		return ErrArrivalNotFound
	}
	now := s.now()
	switch o.Kind {
	case models.PoolLinkObservationOwnerActive:
		if err := s.warmup.RecordOwnerActivity(ctx, m.EmailAccountID, observedAt(o.ObservedAt, now)); err != nil {
			return errx.InternalError()
		}
		return nil
	case models.PoolLinkObservationRemoved:
		err = s.exec.Jobs.PublishJob(ctx, m.EmailAccountID.String(), &models.JobEvent{Type: models.JobEventTypeRemoveEmail,
			Body: &models.JobEventRemoveEmail{UserID: m.UserID, EmailID: m.EmailAccountID, ID: arrival.InternalID}})
	default:
		err = s.exec.Jobs.PublishJob(ctx, m.EmailAccountID.String(), &models.JobEvent{Type: models.JobEventTypeFlagsAdd,
			Body: &models.JobEventFlags{UserID: m.UserID, EmailID: m.EmailAccountID, ID: arrival.InternalID, Flags: []string{"SPAM"}}})
	}
	if err != nil {
		return errx.ErrServiceDown
	}
	return nil
}

// observedAt keeps a reported time within the last hour, so a report can neither backdate nor postdate evidence.
func observedAt(at, now time.Time) time.Time {
	if at.IsZero() || at.After(now) || at.Before(now.Add(-time.Hour)) {
		return now
	}
	return at
}

// executedMailbox is the link's enrolled mailbox, which must be one the instance executes.
func (s *service) executedMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*repository.PoolLinkExecMailbox, *errx.Error) {
	pm, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if pm == nil {
		return nil, ErrMailboxNotFound
	}
	m, err := s.exec.repo.InstanceExecuted(ctx, pm.EmailAccountID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil || m.InstanceID != inst.ID || m.OrganizationID != inst.OrganizationID {
		return nil, ErrMailboxNotSelfExecuted
	}
	return m, nil
}

// SweepSelfExecution closes commands nobody answered in time and reports the actions among them as failed.
func (s *service) SweepSelfExecution(ctx context.Context) error {
	if s.exec == nil {
		return errSelfExecutionDependencies
	}
	for {
		expired, err := s.exec.repo.ExpireCommands(ctx, 500)
		if err != nil {
			return err
		}
		for i := range expired {
			c := &expired[i]
			if c.Kind != models.PoolLinkCommandAction {
				continue
			}
			env, err := openEnvelope(ctx, s.exec.Cipher, c.OrganizationID, c.Payload)
			if err != nil || env.Origin == nil {
				continue
			}
			if err := s.publishJobs(ctx, c.EmailAccountID, actionEvents(env.Origin, models.PoolLinkCommandResult{Outcome: models.PoolLinkOutcomeExpired})); err != nil {
				log.Warn().Err(err).Str("command_id", c.ID.String()).Msg("pool link: expired action outcome not published")
			}
		}
		if len(expired) < 500 {
			return nil
		}
	}
}

// AuditReporterTrust re-decides every instance-executed mailbox's reporter standing.
func (s *service) AuditReporterTrust(ctx context.Context) error {
	if s.exec == nil {
		return errSelfExecutionDependencies
	}
	accounts, err := s.exec.repo.ListInstanceExecutedAccounts(ctx)
	if err != nil {
		return err
	}
	since := s.now().AddDate(0, 0, -config.PoolLinkTrustWindowDays)
	for _, id := range accounts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		current, err := s.exec.repo.ReporterTrustState(ctx, id)
		if err != nil {
			return err
		}
		sample, err := s.exec.repo.SampleReporterTrust(ctx, id, since)
		if err != nil {
			return err
		}
		next := nextReporterTrust(current, sample)
		previous, err := s.exec.repo.SaveReporterTrust(ctx, id, sample, next)
		if err != nil {
			return err
		}
		if next == models.PoolLinkReportUntrusted && previous != models.PoolLinkReportUntrusted && s.exec.Notifier != nil {
			_, expected, divergence := sample.Rates()
			reported, _, _ := sample.Rates()
			s.exec.Notifier.NotifyOperator(opsEventReporterUntrusted, "Linked mailbox placement reports untrusted",
				"A mailbox a linked instance runs reports spam placement far from what Cloud-run recipients saw for the same senders. Its placement reports no longer count toward other mailboxes.",
				map[string]string{"Mailbox": id.String(), "Reported spam rate": percent(reported), "Expected spam rate": percent(expected), "Divergence": percent(divergence)})
		}
	}
	return nil
}

func (s *service) ListSelfExecutionMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkSelfExecutionMailbox, *errx.Error) {
	x, ok := s.repo.(repository.PoolLinkExecRepository)
	if !ok {
		return []models.PoolLinkSelfExecutionMailbox{}, nil
	}
	out, err := x.ListSelfExecutionMailboxes(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

// cancelSelfExecutedCommands cancels what an unenrolled instance-executed mailbox still had queued.
func (s *service) cancelSelfExecutedCommands(ctx context.Context, m models.PoolLinkMailbox) {
	if s.exec == nil || m.Executor.OrCloud() != models.PoolLinkExecutorInstance {
		return
	}
	if err := s.exec.repo.CancelOpenCommands(ctx, m.EmailAccountID); err != nil {
		log.Warn().Err(err).Str("account_id", m.EmailAccountID.String()).Msg("pool link: queued commands not cancelled on unenroll")
	}
}
