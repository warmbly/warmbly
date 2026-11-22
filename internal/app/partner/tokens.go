package partner

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// GoogleTokens brokers Google access tokens for a purchased mailbox; the store and the partner API each have one.
type GoogleTokens interface {
	GoogleToken(ctx context.Context, orgID uuid.UUID, email string, scopes []string) (*oauth2.Token, error)
	Simulated() bool
}

// TokenRouter is the one token source for purchased Google mailboxes: a mailbox a partner attached with the
// token method mints through that partner's callback, any other through Fallback (the store), when set.
type TokenRouter struct {
	Partners *Service
	Fallback GoogleTokens
}

var errNoBroker = errors.New("partner: no token broker for this mailbox")

func (r TokenRouter) GoogleToken(ctx context.Context, orgID uuid.UUID, email string, scopes []string) (*oauth2.Token, error) {
	if r.Partners != nil && r.Partners.mailboxesOn() {
		tok, handled, err := r.Partners.callbackToken(ctx, orgID, email, scopes)
		if handled {
			return tok, err
		}
	}
	if r.Fallback != nil {
		return r.Fallback.GoogleToken(ctx, orgID, email, scopes)
	}
	return nil, errNoBroker
}

// Simulated is the store's development simulator; a partner's callback is always a real broker.
func (r TokenRouter) Simulated() bool { return r.Fallback != nil && r.Fallback.Simulated() }

// callbackToken asks the partner that attached the mailbox for a token; handled is false for a mailbox no partner attached by token.
func (s *Service) callbackToken(ctx context.Context, orgID uuid.UUID, email string, scopes []string) (*oauth2.Token, bool, error) {
	row, err := s.mb.Repo.PartnerTokenMailbox(ctx, orgID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, true, err
	}
	if row == nil || row.PartnerMailboxID == nil {
		return nil, false, nil
	}
	p, err := s.Repo.GetPartner(ctx, row.PartnerID)
	if err != nil {
		return nil, true, err
	}
	if p == nil || p.CallbackBaseURL == nil || *p.CallbackBaseURL == "" {
		return nil, true, errors.New("partner: no token callback is configured")
	}
	key, err := s.CallbackKey(ctx, p.ID)
	if err != nil || key == "" {
		return nil, true, errors.New("partner: the token callback key is not set")
	}
	client := graphkit.New(*p.CallbackBaseURL, key, graphkit.WithHTTPClient(s.HTTP), graphkit.WithRetries(1, 500*time.Millisecond))
	tok, err := client.MailboxAccessToken(ctx, *row.PartnerMailboxID, scopes, "partner:token:"+uuid.NewString())
	if err != nil {
		return nil, true, err
	}
	return &oauth2.Token{AccessToken: tok.AccessToken, TokenType: "Bearer", Expiry: tok.ExpiresAt}, true, nil
}
