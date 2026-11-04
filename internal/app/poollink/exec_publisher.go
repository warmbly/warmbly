package poollink

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// errNoExecutor refuses work addressed to the nil worker that no linked instance runs.
var errNoExecutor = errors.New("pool link: no executor for this work")

// commandEnvelope is a queued command's sealed payload; Origin keeps what the
// result events need and never leaves Cloud.
type commandEnvelope struct {
	Send   *models.PoolLinkSendCommand   `json:"send,omitempty"`
	Action *models.PoolLinkActionCommand `json:"action,omitempty"`
	Origin *models.WarmupEmailAction     `json:"origin,omitempty"`
}

func sealEnvelope(ctx context.Context, c cipher.CipherService, orgID uuid.UUID, env commandEnvelope) (string, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	ci, err := c.Cipher(ctx, orgID)
	if err != nil {
		return "", err
	}
	return ci.Encrypt(ctx, string(raw))
}

func openEnvelope(ctx context.Context, c cipher.CipherService, orgID uuid.UUID, sealed string) (*commandEnvelope, error) {
	ci, err := c.Cipher(ctx, orgID)
	if err != nil {
		return nil, err
	}
	raw, err := ci.Decrypt(ctx, sealed)
	if err != nil {
		return nil, err
	}
	var env commandEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// selfExecPublisher queues work addressed to the nil worker for the linked
// instance that executes the mailbox, and passes everything else through.
type selfExecPublisher struct {
	events.Publisher
	repo   repository.PoolLinkExecRepository
	cipher cipher.CipherService
	now    func() time.Time
}

// NewSelfExecutionPublisher wraps the worker publisher so warmup sends and
// actions for an instance-executed mailbox become queued commands.
func NewSelfExecutionPublisher(inner events.Publisher, repo repository.PoolLinkRepository, c cipher.CipherService) events.Publisher {
	x, ok := repo.(repository.PoolLinkExecRepository)
	if inner == nil || !ok || c == nil {
		return inner
	}
	return &selfExecPublisher{Publisher: inner, repo: x, cipher: c, now: time.Now}
}

func (p *selfExecPublisher) PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *events.SendEmailParams) error {
	if workerID != uuid.Nil {
		return p.Publisher.PublishSendEmail(ctx, workerID, params)
	}
	q, env, err := sendCommand(params, p.now())
	if err != nil {
		return err
	}
	m, err := p.repo.InstanceExecuted(ctx, params.EmailID)
	if err != nil {
		return err
	}
	if m == nil || m.OrganizationID != params.OrgID {
		return errNoExecutor
	}
	q.InstanceID, q.OrganizationID = m.InstanceID, m.OrganizationID
	if q.Payload, err = sealEnvelope(ctx, p.cipher, m.OrganizationID, env); err != nil {
		return err
	}
	// False means an open command already carries this task.
	_, err = p.repo.EnqueueCommand(ctx, q)
	return err
}

// sendCommand is the queued form of a warmup send; anything but one plain warmup message is refused.
func sendCommand(params *events.SendEmailParams, now time.Time) (repository.QueuedPoolLinkCommand, commandEnvelope, error) {
	if params == nil || !params.IsWarmup || len(params.To) != 1 || len(params.CC)+len(params.BCC) > 0 ||
		len(params.Attachments) > 0 || params.TaskID == uuid.Nil || params.MessageID == "" || params.WarmupToken == "" {
		return repository.QueuedPoolLinkCommand{}, commandEnvelope{}, errNoExecutor
	}
	nonce, err := uuid.Parse(params.DispatchNonce)
	if err != nil || nonce == uuid.Nil {
		return repository.QueuedPoolLinkCommand{}, commandEnvelope{}, errNoExecutor
	}
	task := params.TaskID
	env := commandEnvelope{Send: &models.PoolLinkSendCommand{
		To: params.To[0], Subject: params.Subject, BodyPlain: params.BodyPlain, BodyHTML: params.BodyHTML,
		MessageID: params.MessageID, InReplyTo: params.InReplyTo, References: params.References, ThreadID: params.ThreadID,
		WarmupToken: params.WarmupToken, FromName: params.FromName,
	}}
	return repository.QueuedPoolLinkCommand{
		ID: uuid.New(), EmailAccountID: params.EmailID, Kind: models.PoolLinkCommandSend,
		TaskID: &task, DispatchNonce: &nonce,
		NotAfter: now.Add(config.PoolLinkSendCommandTTLMinutes * time.Minute),
	}, env, nil
}

func (p *selfExecPublisher) PublishWarmupAction(ctx context.Context, workerID uuid.UUID, action *models.WarmupEmailAction) error {
	if workerID != uuid.Nil {
		return p.Publisher.PublishWarmupAction(ctx, workerID, action)
	}
	if action == nil || len(action.Actions) == 0 {
		return errNoExecutor
	}
	m, err := p.repo.InstanceExecuted(ctx, action.EmailID)
	if err != nil {
		return err
	}
	if m == nil {
		return errNoExecutor
	}
	var internal *uuid.UUID
	if id, perr := uuid.Parse(action.InternalID); perr == nil {
		internal = &id
	}
	handle, err := p.repo.ArrivalHandle(ctx, m.EmailAccountID, internal, action.RFCMessageID)
	if err != nil {
		return err
	}
	q, env := actionCommand(action, handle, p.now())
	q.InstanceID, q.EmailAccountID, q.OrganizationID = m.InstanceID, m.EmailAccountID, m.OrganizationID
	if q.Payload, err = sealEnvelope(ctx, p.cipher, m.OrganizationID, env); err != nil {
		return err
	}
	_, err = p.repo.EnqueueCommand(ctx, q)
	return err
}

// actionCommand is the queued form of a warmup action; an empty handle addresses the mailbox's own sent copy.
func actionCommand(action *models.WarmupEmailAction, handle string, now time.Time) (repository.QueuedPoolLinkCommand, commandEnvelope) {
	origin := *action
	origin.Actions = slices.Clone(action.Actions)
	env := commandEnvelope{
		Action: &models.PoolLinkActionCommand{
			MessageHandle: handle, RFCMessageID: action.RFCMessageID, Actions: origin.Actions,
			Placement: action.Placement, TargetFolder: action.TargetFolder, Recheck: action.Recheck,
		},
		Origin: &origin,
	}
	return repository.QueuedPoolLinkCommand{
		ID: uuid.New(), Kind: models.PoolLinkCommandAction, DedupeKey: actionDedupeKey(action, handle),
		NotAfter: now.Add(config.PoolLinkActionCommandTTLHours * time.Hour),
	}, env
}

// actionDedupeKey keeps one open command per filing, removal check or deletion of a message.
func actionDedupeKey(action *models.WarmupEmailAction, handle string) string {
	target := handle
	if target == "" {
		target = "sent:" + action.RFCMessageID
	}
	switch {
	case action.FilingID != "":
		return "filing:" + action.FilingID
	case slices.Contains(action.Actions, models.WarmupActionVerifyRemoval):
		return "verify:" + target
	case slices.Contains(action.Actions, models.WarmupActionDelete):
		return "delete:" + target
	}
	return ""
}

func (p *selfExecPublisher) PublishMessageSeen(ctx context.Context, workerID uuid.UUID, action *models.MessageSeenAction) error {
	if workerID == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishMessageSeen(ctx, workerID, action)
}

func (p *selfExecPublisher) PublishMessageFolder(ctx context.Context, workerID uuid.UUID, action *models.MessageFolderAction) error {
	if workerID == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishMessageFolder(ctx, workerID, action)
}

func (p *selfExecPublisher) PublishMailboxIdentity(ctx context.Context, workerID uuid.UUID, body models.EventWorkerMailboxIdentity) error {
	if workerID == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishMailboxIdentity(ctx, workerID, body)
}

func (p *selfExecPublisher) PublishAddEmail(ctx context.Context, workerID uuid.UUID, email *models.AddWorkerEmail) error {
	if workerID == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishAddEmail(ctx, workerID, email)
}

func (p *selfExecPublisher) PublishRemoveEmail(ctx context.Context, workerID uuid.UUID, remove *models.RemoveWorkerEmail) error {
	if workerID == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishRemoveEmail(ctx, workerID, remove)
}

func (p *selfExecPublisher) PublishSyncWake(ctx context.Context, workerID uuid.UUID, wake *models.SyncWake) error {
	if workerID == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishSyncWake(ctx, workerID, wake)
}

func (p *selfExecPublisher) PublishEmailValidation(ctx context.Context, workerID string, body models.EventWorkerEmailValidation) error {
	if id, err := uuid.Parse(workerID); err == nil && id == uuid.Nil {
		return errNoExecutor
	}
	return p.Publisher.PublishEmailValidation(ctx, workerID, body)
}

// JobPublisher hands Cloud's consumer an event exactly as a worker would.
type JobPublisher interface {
	PublishJob(ctx context.Context, key string, event *models.JobEvent) error
}

type busJobPublisher struct {
	bus   eventbus.EventBus
	codec codec.Codec
}

// NewJobPublisher publishes worker events on the bus with the deployment's codec.
func NewJobPublisher(bus eventbus.EventBus, c codec.Codec) JobPublisher {
	if bus == nil || c == nil {
		return nil
	}
	return &busJobPublisher{bus: bus, codec: c}
}

func (p *busJobPublisher) PublishJob(ctx context.Context, key string, event *models.JobEvent) error {
	payload, err := p.codec.Serialize(ctx, kafka.TopicWorkerEvents, event)
	if err != nil {
		return err
	}
	return p.bus.Publish(ctx, kafka.TopicWorkerEvents, key, payload)
}
