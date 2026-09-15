package email

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"golang.org/x/oauth2"
)

// identityReplyWait is how long the backend waits for the worker's answer.
// A little longer than the worker's own deadline, so a slow provider produces
// the worker's message rather than this timeout.
const identityReplyWait = 10 * time.Second

// readSendIdentity asks the worker holding the mailbox to read its send-as
// identities from the provider, and waits for the answer.
//
// The control plane deliberately does not make this call itself. The worker is
// what holds the mailbox and what the provider has learned to see it from:
// reading a customer's settings from here would show Google a second client
// address for the same mailbox, which is what earns a sign-in challenge, and
// it would mean decrypting a mailbox credential in the control plane for
// something the execution plane already has in hand.
func (s *emailService) readSendIdentity(ctx context.Context, acc *models.Email, wantSignature bool) (*models.MailboxIdentityResult, *errx.Error) {
	if s.publisher == nil || s.r == nil {
		return nil, errx.ErrEmailIdentityUnavailable
	}
	if acc.WorkerID == nil {
		// Not placed yet, or just unassigned. There is no machine to ask.
		return nil, errx.ErrEmailIdentityUnavailable
	}

	processID := uuid.New()
	sub := s.r.Subscribe(ctx, "mailbox_identity:"+processID.String())
	defer sub.Close()

	if err := s.publisher.PublishMailboxIdentity(ctx, *acc.WorkerID, models.EventWorkerMailboxIdentity{
		EmailID:       acc.ID,
		ProcessID:     processID,
		WantSignature: wantSignature,
		SignatureFor:  acc.SendAsEmail,
	}); err != nil {
		errs.CaptureException(err)
		return nil, errx.ErrEmailIdentityUnavailable
	}

	waitCtx, cancel := context.WithTimeout(ctx, identityReplyWait)
	defer cancel()

	msg, err := sub.ReceiveMessage(waitCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn().Str("email_account_id", acc.ID.String()).Msg("no answer from the worker holding the mailbox")
			return nil, errx.ErrEmailIdentityUnavailable
		}
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	var res models.MailboxIdentityResult
	if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if !res.OK {
		log.Warn().Str("email_account_id", acc.ID.String()).Str("reason", res.Error).Msg("worker could not read the mailbox's sending identity")
		return nil, errx.ErrEmailIdentityUnavailable
	}
	return &res, nil
}

// handshakeGmailClient builds a Gmail client on the token the consent
// handshake just produced, for the one call the control plane still makes.
// Nothing is persisted through it: no refresh hook is attached, so it cannot
// write a token back, and it lives only as long as the connect.
func (s *emailService) handshakeGmailClient(ctx context.Context, tok *oauth2.Token) *goog.Client {
	if tok == nil {
		return nil
	}
	cfg, xerr := s.oauthConfigFor(models.InboxProviderGoogle)
	if xerr != nil {
		return nil
	}
	client := &goog.Client{}
	if merr := client.Init(ctx, tok, *cfg); merr != nil {
		log.Warn().Str("error", merr.Message).Msg("could not build a Gmail client for the connect-time identity read")
		return nil
	}
	return client
}

// importedSignature turns the provider's signature into what is stored, or
// nothing at all. An empty signature at the provider is a real answer and must
// not overwrite one written here.
func importedSignature(html string) (*models.ImportedSignature, *errx.Error) {
	html = strings.TrimSpace(html)
	if html == "" {
		return nil, nil
	}
	// Characters, not bytes, matching what the column accepts on the way in.
	if utf8.RuneCountInString(html) > config.SignatureHTMLMax {
		return nil, errx.ErrEmailSignatureTooLarge
	}
	plain := strings.TrimSpace(mailhtml.ToText(html))
	if r := []rune(plain); len(r) > config.SignaturePlainMax {
		plain = string(r[:config.SignaturePlainMax])
	}
	return &models.ImportedSignature{HTML: html, Plain: plain}, nil
}

// GetSendIdentity reports the mailbox's stored sending identity. Read-only: it
// never calls the provider, so a dashboard open costs nothing at Google.
func (s *emailService) GetSendIdentity(ctx context.Context, orgID, emailAccountID string) (*models.SendIdentity, *errx.Error) {
	return s.emailRepository.GetSendIdentity(ctx, orgID, emailAccountID)
}

// RefreshSendIdentity re-reads the send-as list from the provider and stores
// it, importing the signature too when asked. The read itself happens on the
// worker; this decides what to keep.
func (s *emailService) RefreshSendIdentity(ctx context.Context, orgID, emailAccountID string, importSignature bool) (*models.SendIdentity, *errx.Error) {
	current, xerr := s.emailRepository.GetSendIdentity(ctx, orgID, emailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if !current.Supported {
		return nil, errx.ErrEmailSendAsUnsupported
	}

	accountID, perr := uuid.Parse(emailAccountID)
	if perr != nil {
		return nil, errx.ErrUuid
	}
	acc, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}

	res, xerr := s.readSendIdentity(ctx, acc, importSignature)
	if xerr != nil {
		return nil, xerr
	}

	var sig *models.ImportedSignature
	if importSignature {
		sig, xerr = importedSignature(res.SignatureHTML)
		if xerr != nil {
			return nil, xerr
		}
	}

	if xerr := s.emailRepository.SetSendIdentity(ctx, accountID, res.Identities, sig); xerr != nil {
		return nil, xerr
	}
	return s.emailRepository.GetSendIdentity(ctx, orgID, emailAccountID)
}

// captureSendIdentity records the send-as list (and the signature) on a fresh
// connect, while the OAuth handshake's own token is still in hand.
//
// This is the one moment the control plane may make the call: the mailbox has
// no worker yet, the token came from the consent the customer just completed
// rather than from storage, and the provider already saw this address when the
// code was exchanged. Everything afterwards goes through the worker.
func (s *emailService) captureSendIdentity(ctx context.Context, acc *models.Email, tok *oauth2.Token) {
	s.storeSendIdentity(ctx, acc, tok, true)
}

// captureSendIdentityList refreshes the send-as list without touching the
// stored signature, for a reconnect.
func (s *emailService) captureSendIdentityList(ctx context.Context, acc *models.Email, tok *oauth2.Token) {
	s.storeSendIdentity(ctx, acc, tok, false)
}

func (s *emailService) storeSendIdentity(ctx context.Context, acc *models.Email, tok *oauth2.Token, importSignature bool) {
	if acc == nil || models.InboxProvider(acc.Provider) != models.InboxProviderGoogle {
		return
	}
	client := s.handshakeGmailClient(ctx, tok)
	if client == nil {
		return
	}
	rows, err := client.ListSendAs(ctx)
	if err != nil {
		log.Warn().Err(err).Str("email_account_id", acc.ID.String()).Msg("could not read Gmail send-as addresses on connect")
		return
	}
	var sig *models.ImportedSignature
	if importSignature {
		// A signature too large to store keeps the generated default rather
		// than failing a connect over it.
		sig, _ = importedSignature(goog.SignatureFor(rows, acc.SendAsEmail))
	}
	if xerr := s.emailRepository.SetSendIdentity(ctx, acc.ID, goog.SendAsIdentities(rows), sig); xerr != nil {
		log.Warn().Str("email_account_id", acc.ID.String()).Msg("could not store Gmail send-as addresses")
	}
}

// validateSendAsChoice refuses an alias the provider has not verified. The
// mailbox's own address is always legal and is what an empty value means.
func (s *emailService) validateSendAsChoice(ctx context.Context, orgID, emailAccountID string, choice string) *errx.Error {
	want := strings.ToLower(strings.TrimSpace(choice))
	if want == "" {
		return nil
	}
	current, xerr := s.emailRepository.GetSendIdentity(ctx, orgID, emailAccountID)
	if xerr != nil {
		return xerr
	}
	if !current.Supported {
		return errx.ErrEmailSendAsUnsupported
	}
	if want == strings.ToLower(strings.TrimSpace(current.MailboxEmail)) {
		return nil
	}
	for _, id := range current.Identities {
		if id.Verified && strings.ToLower(id.Email) == want {
			return nil
		}
	}
	return errx.ErrEmailSendAsUnknown
}
