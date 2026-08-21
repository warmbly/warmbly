package wmail

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/client/smtpimap/smtp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

type GoogleService struct {
	Token    *oauth2.Token
	svc      *goog.Client
	OnUpdate func(token *oauth2.Token)
}

type OutlookService struct {
	Token    *oauth2.Token
	OnUpdate func(token *oauth2.Token)
}

type GoogleData struct {
	Client        *goog.Client
	LastHistoryID uint64
}

type GraphData struct {
	Client *msgraph.Client
}

type SmtpImapData struct {
	ImapClient *imap.Client
	SmtpClient *smtp.Client
	Mailboxes  []*models.Mailbox
	mailbox    uint32
}

type WMail struct {
	UserID uuid.UUID
	ID     uuid.UUID
	// OrgID scopes the organization-wide sync budget; nil for a legacy
	// personal mailbox.
	OrgID *uuid.UUID

	Email          string
	FirstName      string
	LastName       string
	SignaturePlain string
	SignatureHTML  string

	// SaveToSent files a copy of every outbound message in the mailbox's Sent
	// folder. SMTP/IMAP only: Gmail and Graph file their own copy, so acting on
	// it there would duplicate every sent message.
	SaveToSent bool

	EmailType models.InboxProvider

	GoogleData   *GoogleData
	GraphData    *GraphData
	SmtpImapData *SmtpImapData

	Cache                     *cache.Cache
	Storage                   storage.Store
	EmailMessageMapRepository repository.EmailMessageMapRepository
	SyncContext               repository.SyncContextRepository
	CipherService             cipher.CipherService

	// Sync fair use: the budget engine and the relayed state (governor.go,
	// sync_state.go). Built in NewWMail from the ADD_EMAIL payload.
	gov     *governor
	tracker *syncTracker
	// laneCache remembers deferred messages' lanes across passes; googleTick
	// and graphTick carry the running pass's stats into provider callbacks.
	laneCache  laneCache
	googleTick *tickStats
	graphTick  *tickStats

	Ctx           context.Context
	Cancel        context.CancelFunc
	TerminateFunc func()

	onEvent func(jobType models.JobEventType, body any) error
}

func NewWMail(
	data *models.AddWorkerEmail,
	OnEvent func(eventType models.JobEventType, key string, body any) error,
	terminate func(),
	cache *cache.Cache, storage storage.Store,
	emailMessageMapRepository repository.EmailMessageMapRepository,
	syncContext repository.SyncContextRepository,
	cipherService cipher.CipherService,
) (*WMail, *errx.MailError) {
	// Use background context so the WMail outlives the AddEmail request handler.
	mailCtx, cancel := context.WithCancel(context.Background())

	mail := &WMail{
		ID:        data.ID,
		UserID:    data.UserID,
		OrgID:     data.OrganizationID,
		Email:     data.Email,
		FirstName: data.FirstName,
		LastName:  data.LastName,
		EmailType: data.Type,
		// Unset in the payload means yes; see AddWorkerEmail.SavesSentCopy.
		SaveToSent: data.Type == models.InboxProviderSMTPIMAP && data.SavesSentCopy(),
		onEvent: func(jobType models.JobEventType, body any) error {
			return OnEvent(jobType, data.ID.String(), body)
		},

		Ctx:           mailCtx,
		Cancel:        cancel,
		TerminateFunc: terminate,

		Cache:                     cache,
		Storage:                   storage,
		EmailMessageMapRepository: emailMessageMapRepository,
		SyncContext:               syncContext,
		CipherService:             cipherService,
	}

	// A publisher older than the sync policy sends no Sync block; compiled
	// defaults then apply and the mailbox starts its backfill from scratch.
	var seed *models.SyncState
	var policy models.SyncPolicy
	if data.Sync != nil {
		policy = data.Sync.Policy
		seed = data.Sync.State
	}
	// A nil *cache.Cache must not become a non-nil interface holding nil.
	var gcache redisCmdable
	if cache != nil {
		gcache = cache
	}
	mail.gov = newGovernor(data.ID, data.OrganizationID, gcache, policy)
	mail.tracker = newSyncTracker(seed, func(st models.SyncState) error {
		return mail.onEvent(models.JobEventTypeSyncState, &models.JobEventSyncState{
			UserID:  mail.UserID,
			EmailID: mail.ID,
			State:   st,
		})
	})

	switch data.Type {
	case models.InboxProviderGoogle:
		if data.Google == nil {
			return nil, errx.MError(
				errx.MailErrorCritical,
				errx.MailErrorCodeAuthenticationFailed,
				"missing Google credentials in add-email payload",
				errx.MailErrorResolveMethodReload,
			)
		}
		mail.GoogleData = &GoogleData{
			Client: &goog.Client{
				Email:     data.Email,
				FirstName: data.FirstName,
				LastName:  data.LastName,

				Cache:           mail.Cache,
				OnMessageAdded:  mail.onGoogleMessageAdded,
				OnMessageRemove: mail.onGoogleMessageRemove,
				OnLabelAdd:      mail.onGoogleMessageLabelsAdded,
				OnLabelRemove:   mail.onGoogleMessageLabelsRemoved,
				// Without this a refreshed Gmail token is never persisted, so
				// the mailbox stops working roughly an hour after connect.
				OnTokenRefresh: func(_ context.Context, t *oauth2.Token) error {
					return mail.onTokenUpdate(t)
				},
			},
			LastHistoryID: data.Google.LastHistoryID,
		}

		if err := mail.GoogleData.Client.Init(mailCtx, data.Google.Token, data.Cfg); err != nil {
			return nil, err
		}
	case models.InboxProviderOutlook:
		// Microsoft/Outlook mailboxes run entirely on Microsoft Graph: RAW MIME
		// sendMail plus delta-based inbound sync. There is no IMAP/SMTP path.
		if data.Graph == nil {
			return nil, errx.MError(
				errx.MailErrorCritical,
				errx.MailErrorCodeAuthenticationFailed,
				"missing Microsoft Graph credentials in add-email payload",
				errx.MailErrorResolveMethodReload,
			)
		}
		token := data.Graph.Token
		deltaLinks := data.Graph.DeltaLinks

		mail.GraphData = &GraphData{
			Client: &msgraph.Client{
				Email:     data.Email,
				FirstName: data.FirstName,
				LastName:  data.LastName,

				Cache:      mail.Cache,
				DeltaLinks: cloneStringMap(deltaLinks),

				OnMessageSeen:   mail.onGraphMessageSeen,
				OnMessageRemove: mail.onGraphMessageRemove,
				OnDelta:         mail.onGraphDelta,
				OnTokenRefresh: func(_ context.Context, t *oauth2.Token) error {
					return mail.onTokenUpdate(t)
				},
			},
		}

		if err := mail.GraphData.Client.Init(mailCtx, token, data.Cfg); err != nil {
			return nil, err
		}
	case models.InboxProviderSMTPIMAP:
		// Generic SMTP/IMAP with plain-auth credentials (arbitrary providers).
		if data.SmtpImap == nil || data.SmtpImap.Credentials == nil {
			return nil, errx.MError(
				errx.MailErrorCritical,
				errx.MailErrorCodeInvalidCredentials,
				"missing SMTP/IMAP credentials in add-email payload",
				errx.MailErrorResolveMethodReload,
			)
		}
		mail.SmtpImapData = &SmtpImapData{}

		if data.ImapSync {
			mail.SmtpImapData.ImapClient = &imap.Client{
				Email:       data.Email,
				AuthType:    models.AuthPlain,
				Credentials: data.SmtpImap.Credentials.IMAP,
			}
			if err := mail.SmtpImapData.ImapClient.Connect(); err != nil {
				return nil, err
			}
			// Saved folder cursors: live sync resumes from each folder's stored
			// HIGHESTMODSEQ instead of re-baselining (and, before this, instead
			// of re-walking every folder on every worker restart).
			for i := range data.SmtpImap.Mailboxes {
				box := data.SmtpImap.Mailboxes[i]
				mail.SmtpImapData.Mailboxes = append(mail.SmtpImapData.Mailboxes, &box)
			}
		}

		mail.SmtpImapData.SmtpClient = &smtp.Client{
			FirstName:   data.FirstName,
			LastName:    data.LastName,
			Email:       data.Email,
			AuthType:    models.AuthPlain,
			Credentials: data.SmtpImap.Credentials.SMTP,
		}
	default:
		return nil, errx.MError(
			errx.MailErrorCritical,
			errx.MailErrorCodeUnsupported,
			"Unsupported email provider",
			errx.MailErrorResolveMethodReload,
		)
	}

	return mail, nil
}

// ApplySyncPolicy takes the budget from a republished ADD_EMAIL for a mailbox
// that is already loaded, so an operator's settings change lands within the
// reconciler's republish interval instead of at the next worker restart. The
// backfill window already fixed by a running import is deliberately not moved.
func (w *WMail) ApplySyncPolicy(data *models.AddWorkerEmailSyncData) {
	if data == nil || w.gov == nil {
		return
	}
	w.gov.SetPolicy(data.Policy)
}
