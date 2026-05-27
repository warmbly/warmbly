package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

type Client struct {
	Email       string
	AuthType    models.AuthType
	Credentials *models.Service
	Oauth2      *models.Oauth2Service

	client *imapclient.Client

	// mu serializes commands that change SELECTed mailbox or mutate state.
	// Warmup actions (MOVE/STORE) run on a different code path than the sync
	// loop and must not interleave with FetchChanges.
	mu sync.Mutex

	OnUpdate func(ctx context.Context, email *models.EmailMessageData) error
}

func (c *Client) Connect() *errx.MailError {
	var addr string
	switch c.AuthType {
	case models.AuthPlain:
		addr = fmt.Sprintf("%s:%d", c.Credentials.Host, c.Credentials.Port)
	case models.AuthOAuth2:
		addr = fmt.Sprintf("%s:%d", c.Oauth2.Host, c.Oauth2.Port)
	}

	conn, err := tls.Dial("tcp", addr, nil)
	if err != nil {
		return errx.ErrMailServerUnreachable
	}

	client := imapclient.New(conn, nil)

	caps := client.Caps()
	if !caps.Has(imap.CapCondStore) {
		return errx.ErrMailCondStoreNotSupported
	}

	var xerr *errx.MailError

	switch c.AuthType {
	case models.AuthPlain:
		xerr = c.plainAuth()
	case models.AuthOAuth2:
		xerr = c.oauth2Auth()
	}

	return xerr
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) plainAuth() *errx.MailError {
	if err := c.client.Login(c.Credentials.Username, c.Credentials.Password).Wait(); err != nil {
		return c.handleError(err)
	}

	return nil
}

func (c *Client) oauth2Auth() *errx.MailError {
	tk, err := c.Oauth2.Token.Token()
	if err != nil {
		var rErr *oauth2.RetrieveError
		if errors.As(err, &rErr) {
			if rErr.Response.StatusCode >= 500 {
				return errx.ErrMailServerUnreachable
			}
		}
		return errx.ErrMailAuthenticationFailed
	}

	saslc := sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
		Username: c.Email,
		Token:    tk.AccessToken,
		Port:     c.Oauth2.Port,
		Host:     c.Oauth2.Host,
	})

	if err := c.client.Authenticate(saslc); err != nil {
		return c.handleError(err)
	}

	return nil
}

func (c *Client) Folders() ([]models.Mailbox, *errx.MailError) {
	var resp []models.Mailbox

	cmd := c.client.List("", "%", nil)

	for f := cmd.Next(); f != nil; f = cmd.Next() {
		if len(resp) >= config.MaxEmailFolders {
			return nil, errx.ErrMailFoldersMax
		}

		var attrs []string = make([]string, len(f.Attrs))

		for i := range f.Attrs {
			attrs[i] = string(f.Attrs[i])
		}

		if f.Status == nil {
			continue
		}

		resp = append(resp, models.Mailbox{
			Name:          f.Mailbox,
			Attrs:         attrs,
			UIDValidity:   f.Status.UIDValidity,
			HighestModSeq: f.Status.HighestModSeq,
		})
	}

	if err := cmd.Close(); err != nil {
		return nil, c.handleError(err)
	}

	return resp, nil
}

func (c *Client) Mailbox(mailbox string, uidvali, opts *imap.SelectOptions) error {
	if _, err := c.client.Select(mailbox, opts).Wait(); err != nil {
		return err
	}

	return nil
}

func (c *Client) FetchChanges(ctx context.Context, lastModSeq uint64) *errx.MailError {
	cmd := c.client.Fetch(&imap.SeqSet{}, &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		BodyStructure: &imap.FetchItemBodyStructure{
			Extended: true,
		},
		Flags:        true,
		ModSeq:       true,
		InternalDate: true,
		ChangedSince: lastModSeq,
	})
	for em := cmd.Next(); em != nil; em = cmd.Next() {
		var email models.EmailMessageData
		var euid imap.UID

		var bodyStructure imap.BodyStructure

		for item := em.Next(); item != nil; item = em.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				email.UID = uint32(item.UID)
				euid = item.UID
			case imapclient.FetchItemDataFlags:
				email.Flags = make([]string, 0)
				for _, f := range item.Flags {
					email.Flags = append(email.Flags, string(f))
				}
			case imapclient.FetchItemDataEnvelope:
				email.BCC = GetAddressNames(item.Envelope.Bcc)
				email.CC = GetAddressNames(item.Envelope.Cc)
				email.Date = item.Envelope.Date
				email.From = GetAddressNames(item.Envelope.From)
				email.InReplyTo = item.Envelope.InReplyTo
				email.MessageID = item.Envelope.MessageID
				email.ReplyTo = GetAddressNames(item.Envelope.ReplyTo)
				email.Sender = GetAddressNames(item.Envelope.Sender)
				email.Subject = item.Envelope.Subject
				email.To = GetAddressNames(item.Envelope.To)
			case imapclient.FetchItemDataRFC822Size:
				email.Size = item.Size
			case imapclient.FetchItemDataInternalDate:
				email.InternalDate = item.Time
			case imapclient.FetchItemDataModSeq:
				email.ModSeq = item.ModSeq
			case imapclient.FetchItemDataBodyStructure:
				bodyStructure = item.BodyStructure
			}
		}

		email.BodyPlain, email.BodyHTML = fetchTextParts(c.client, euid, bodyStructure)

		if err := c.OnUpdate(ctx, &email); err != nil {
			log.Warn().Err(err).Msg("Failed to Send Message Update")
			return nil
		}
	}

	if err := cmd.Close(); err != nil {
		return c.handleError(err)
	}

	return nil
}
