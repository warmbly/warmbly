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

	Email          string
	FirstName      string
	LastName       string
	SignaturePlain string
	SignatureHTML  string

	EmailType models.InboxProvider

	GoogleData   *GoogleData
	GraphData    *GraphData
	SmtpImapData *SmtpImapData

	Cache                     *cache.Cache
	Storage                   storage.Store
	EmailMessageMapRepository repository.EmailMessageMapRepository
	CipherService             cipher.CipherService

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
	cipherService cipher.CipherService,
) (*WMail, *errx.MailError) {
	// Use background context so the WMail outlives the AddEmail request handler.
	mailCtx, cancel := context.WithCancel(context.Background())

	mail := &WMail{
		ID:        data.ID,
		UserID:    data.UserID,
		Email:     data.Email,
		FirstName: data.FirstName,
		LastName:  data.LastName,
		EmailType: data.Type,
		onEvent: func(jobType models.JobEventType, body any) error {
			return OnEvent(jobType, data.ID.String(), body)
		},

		Ctx:           mailCtx,
		Cancel:        cancel,
		TerminateFunc: terminate,

		Cache:                     cache,
		Storage:                   storage,
		EmailMessageMapRepository: emailMessageMapRepository,
		CipherService:             cipherService,
	}

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
				OnMessageAdd:    mail.onGoogleMessageAdd,
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

				OnMessageAdd:    mail.onGraphMessageAdd,
				OnMessageRemove: mail.onGraphMessageRemove,
				OnFlagsChange:   mail.onGraphFlagsChange,
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

				OnUpdate: mail.onImapEmailUpdate,
			}
			if err := mail.SmtpImapData.ImapClient.Connect(); err != nil {
				return nil, err
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
