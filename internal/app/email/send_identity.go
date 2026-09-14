package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

// The Gmail settings endpoint behind gmail.settings.basic. It answers with
// every identity the account may send as, each carrying the display name and
// signature the customer configured in Gmail.
const gmailSendAsURL = "https://gmail.googleapis.com/gmail/v1/users/me/settings/sendAs"

// gmailSendAs is the provider's shape, narrowed to what is used. Gmail omits
// verificationStatus on the primary address (it cannot be unverified), so an
// absent value is not a failed verification.
type gmailSendAs struct {
	SendAsEmail        string `json:"sendAsEmail"`
	DisplayName        string `json:"displayName"`
	Signature          string `json:"signature"`
	IsPrimary          bool   `json:"isPrimary"`
	IsDefault          bool   `json:"isDefault"`
	VerificationStatus string `json:"verificationStatus"`
}

func (g gmailSendAs) verified() bool {
	return g.IsPrimary || g.VerificationStatus == "accepted"
}

// fetchGmailSendAs reads the mailbox's send-as identities. The raw provider
// rows are returned alongside the normalized list because the signature and
// display name are picked from them by the caller, which knows which identity
// the mailbox actually sends from.
func fetchGmailSendAs(ctx context.Context, token string) ([]gmailSendAs, *errx.Error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, gmailSendAsURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, sendAsLookupFailed(ctx, "transport", 0, nil, err)
	}
	defer resp.Body.Close()
	// A signature is prose with markup and several identities can carry one,
	// so this payload is a different size class from the profile endpoint's.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSendAsBody))
	if resp.StatusCode != http.StatusOK {
		return nil, sendAsLookupFailed(ctx, "status", resp.StatusCode, body, nil)
	}
	var out struct {
		SendAs []gmailSendAs `json:"sendAs"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, sendAsLookupFailed(ctx, "decode", resp.StatusCode, body, err)
	}
	return out.SendAs, nil
}

// maxSendAsBody bounds the send-as payload. Gmail caps one signature at 10,000
// characters and an account at 99 send-as addresses; this covers both with
// room, and is still a bound rather than an open read.
const maxSendAsBody = 2 << 20

func sendAsLookupFailed(ctx context.Context, stage string, status int, body []byte, cause error) *errx.Error {
	detail := diagnosticBody(status, body)
	opts := []errs.Option{
		errs.Tag("provider", "gmail"),
		errs.Tag("stage", stage),
		errs.Extra("status", status),
	}
	if detail != "" {
		opts = append(opts, errs.Extra("response", detail))
	}
	if cause != nil {
		errs.CaptureExceptionContext(ctx, cause, opts...)
	} else {
		errs.CaptureMessageContext(ctx, "gmail send-as lookup failed at "+stage, opts...)
	}
	log.Warn().Str("stage", stage).Int("status", status).Msg("mailbox identity: Gmail would not list send-as addresses")
	return errx.New(errx.BadRequest,
		"Warmbly could not read this mailbox's sending addresses from Google. Reconnect the mailbox and leave all the permissions ticked, then try again.")
}

// normalizeSendAs turns the provider's rows into what is stored. Addresses are
// lowercased because that is what every comparison against them does.
func normalizeSendAs(rows []gmailSendAs) []models.SendAsIdentity {
	out := make([]models.SendAsIdentity, 0, len(rows))
	for _, r := range rows {
		addr := strings.ToLower(strings.TrimSpace(r.SendAsEmail))
		if addr == "" {
			continue
		}
		out = append(out, models.SendAsIdentity{
			Email:     addr,
			Name:      strings.TrimSpace(r.DisplayName),
			IsPrimary: r.IsPrimary,
			IsDefault: r.IsDefault,
			Verified:  r.verified(),
		})
	}
	return out
}

// pickSignature chooses whose signature to import: the identity the mailbox
// sends from, then the provider's default, then the primary. A mailbox
// sending as an alias signs off as that alias, which is the whole point of
// having picked one.
func pickSignature(rows []gmailSendAs, sendAs string) (*models.ImportedSignature, *errx.Error) {
	want := strings.ToLower(strings.TrimSpace(sendAs))
	var chosen *gmailSendAs
	for i := range rows {
		r := &rows[i]
		addr := strings.ToLower(strings.TrimSpace(r.SendAsEmail))
		if want != "" && addr == want {
			chosen = r
			break
		}
		if chosen != nil {
			continue
		}
		if r.IsDefault || r.IsPrimary {
			chosen = r
		}
	}
	if chosen == nil {
		return nil, nil
	}
	html := strings.TrimSpace(chosen.Signature)
	if html == "" {
		// An empty signature in Gmail is a real answer, not a failure, and
		// overwriting a signature written here with nothing would be a
		// surprising way to lose it.
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
// it, importing the signature too when asked. This is the only path that talks
// to Google, and it is a write: the stored list is what the alias choice is
// validated against.
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
	tok, xerr := s.OAuthAccessToken(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}

	rows, xerr := fetchGmailSendAs(ctx, tok.AccessToken)
	if xerr != nil {
		return nil, xerr
	}

	var sig *models.ImportedSignature
	if importSignature {
		sig, xerr = pickSignature(rows, current.SendAsEmail)
		if xerr != nil {
			return nil, xerr
		}
	}

	if xerr := s.emailRepository.SetSendIdentity(ctx, accountID, normalizeSendAs(rows), sig); xerr != nil {
		return nil, xerr
	}
	return s.emailRepository.GetSendIdentity(ctx, orgID, emailAccountID)
}

// captureSendIdentity records the send-as list (and the signature) on a fresh
// connect, so a mailbox arrives knowing what it may send as and signing off
// the way its owner already signs off in Gmail.
//
// Best-effort on purpose: the mailbox is connected and working by this point,
// and a settings read that fails must not undo that. The customer can press
// refresh, and nothing here is load-bearing for sending.
func (s *emailService) captureSendIdentity(ctx context.Context, acc *models.Email, accessToken string) {
	s.storeSendIdentity(ctx, acc, accessToken, true)
}

// captureSendIdentityList refreshes the send-as list without touching the
// stored signature, for a reconnect.
func (s *emailService) captureSendIdentityList(ctx context.Context, acc *models.Email, accessToken string) {
	s.storeSendIdentity(ctx, acc, accessToken, false)
}

func (s *emailService) storeSendIdentity(ctx context.Context, acc *models.Email, accessToken string, importSignature bool) {
	if acc == nil || models.InboxProvider(acc.Provider) != models.InboxProviderGoogle {
		return
	}
	rows, xerr := fetchGmailSendAs(ctx, accessToken)
	if xerr != nil {
		log.Warn().Str("email_account_id", acc.ID.String()).Msg("could not read Gmail send-as addresses")
		return
	}
	var sig *models.ImportedSignature
	if importSignature {
		// A signature too large to store keeps the generated default rather
		// than failing a connect over it.
		sig, _ = pickSignature(rows, "")
	}
	if xerr := s.emailRepository.SetSendIdentity(ctx, acc.ID, normalizeSendAs(rows), sig); xerr != nil {
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
