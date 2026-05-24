package wmail

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/client/smtpimap/smtp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/stoken"
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
	SmtpImapData *SmtpImapData

	Cache                     *cache.Cache
	Storage                   *storage.Client
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
	cache *cache.Cache, storage *storage.Client,
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
			},
			LastHistoryID: data.Google.LastHistoryID,
		}

		if err := mail.GoogleData.Client.Init(mailCtx, data.Google.Token, data.Cfg); err != nil {
			return nil, err
		}
	default:
		mail.SmtpImapData = &SmtpImapData{}
		var smtpOauth2 *models.Oauth2Service
		var imapOauth2 *models.Oauth2Service

		var authType models.AuthType

		switch data.Type {
		case models.InboxProviderOutlook:
			imapOauth2 = &models.Oauth2Service{
				Host: "outlook.office365.com",
				Port: 993,
			}
			smtpOauth2 = &models.Oauth2Service{
				Host: "smtp-mail.outlook.com",
				Port: 587,
			}
			authType = models.AuthOAuth2
		case models.InboxProviderSMTPIMAP:
			authType = models.AuthPlain
		}

		if data.SmtpImap.Token != nil {
			ts := data.Cfg.TokenSource(mailCtx, data.SmtpImap.Token)
			ts = oauth2.ReuseTokenSource(data.SmtpImap.Token, ts)
			ts = stoken.New(ts, func(token *oauth2.Token) error {
				return mail.onTokenUpdate(token)
			})

			tk := stoken.New(ts, mail.onTokenUpdate)

			if imapOauth2 != nil {
				imapOauth2.Token = tk
			}
			if smtpOauth2 != nil {
				smtpOauth2.Token = tk
			}
		}

		if data.ImapSync {
			var imapCredentials *models.Service
			if authType == models.AuthPlain {
				imapCredentials = data.SmtpImap.Credentials.IMAP
			}
			mail.SmtpImapData.ImapClient = &imap.Client{
				Email:       data.Email,
				AuthType:    authType,
				Credentials: imapCredentials,
				Oauth2:      imapOauth2,

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
			AuthType:    authType,
			Credentials: data.SmtpImap.Credentials.SMTP,
			Oauth2:      smtpOauth2,
		}
	}

	return mail, nil
}
