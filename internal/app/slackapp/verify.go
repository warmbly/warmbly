package slackapp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Sign in with Slack (OpenID Connect) proves which Slack account the person on
// the link page controls, so a link can be made when the Slack and Warmbly
// emails differ or Slack does not share the email.

const (
	oidcAuthorizeURL = "https://slack.com/openid/connect/authorize"
	oidcIssuer       = "https://slack.com"
	verifyStateTTL   = 10 * time.Minute
)

// LinkProof is the Sign in with Slack result the link page sends back.
type LinkProof struct {
	Code  string
	State string
}

type verifyState struct {
	CodeHash     string    `json:"h"`
	UserID       uuid.UUID `json:"u"`
	Nonce        string    `json:"n"`
	ReturnOrigin string    `json:"return_origin,omitempty"`
}

func (s *Service) OAuthReturnOrigin(ctx context.Context, state string) string {
	if s.guard == nil {
		return ""
	}
	var st verifyState
	if json.Unmarshal([]byte(s.guard.get(ctx, verifyKey(state))), &st) != nil {
		return ""
	}
	if st.ReturnOrigin == "" {
		return config.PrimaryDashboardOrigin()
	}
	return config.TrustedOrigin(st.ReturnOrigin)
}

func verifyKey(state string) string { return "slack:link:verify:" + state }

func randToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// VerifyAvailable reports whether Sign in with Slack can confirm a link.
func (s *Service) VerifyAvailable() bool {
	id, secret := s.integ.SlackOAuthClient()
	return id != "" && secret != "" && s.integ.SlackOAuthRedirectURL() != ""
}

// StartLinkVerify returns the Sign in with Slack URL that proves the caller
// controls the Slack account a pending link code names.
func (s *Service) StartLinkVerify(ctx context.Context, userID uuid.UUID, code string) (string, *errx.Error) {
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return "", ErrSlackLinkInvalid
	}
	if !s.VerifyAvailable() {
		return "", ErrSlackVerifyUnavailable
	}
	c, err := s.repo.PreviewLinkCode(ctx, hashLinkCode(code))
	if err != nil {
		return "", errx.InternalError()
	}
	if c == nil {
		return "", ErrSlackLinkInvalid
	}
	if m, xerr := s.orgs.GetMembership(ctx, c.OrganizationID, userID); xerr != nil || m == nil || m.AcceptedAt == nil {
		return "", errSlackLinkNotMember
	}
	state, err := randToken()
	if err != nil {
		return "", errx.InternalError()
	}
	nonce, err := randToken()
	if err != nil {
		return "", errx.InternalError()
	}
	blob, _ := json.Marshal(verifyState{CodeHash: hex.EncodeToString(hashLinkCode(code)), UserID: userID, Nonce: nonce, ReturnOrigin: config.DashboardOriginFromContext(ctx)})
	s.guard.put(ctx, verifyKey(state), string(blob), verifyStateTTL)

	clientID, _ := s.integ.SlackOAuthClient()
	q := url.Values{
		"response_type": {"code"},
		"scope":         {"openid"},
		"client_id":     {clientID},
		"redirect_uri":  {s.integ.SlackOAuthRedirectURL()},
		"state":         {state},
		"nonce":         {nonce},
		"team":          {c.SlackTeamID},
	}
	return oidcAuthorizeURL + "?" + q.Encode(), nil
}

// verifyProof checks a Sign in with Slack result against the link code: the
// flow was started by this user for this code, and Slack signed in the code's
// Slack account.
func (s *Service) verifyProof(ctx context.Context, userID uuid.UUID, code string, c *models.SlackLinkCode, p LinkProof) *errx.Error {
	if p.State == "" {
		return ErrSlackVerifyFailed
	}
	// Taken in one step, so a state completes at most one verification.
	raw := s.guard.take(ctx, verifyKey(p.State))
	if raw == "" {
		return ErrSlackVerifyFailed
	}
	var st verifyState
	if json.Unmarshal([]byte(raw), &st) != nil || st.UserID != userID || st.CodeHash != hex.EncodeToString(hashLinkCode(code)) {
		return ErrSlackVerifyFailed
	}
	clientID, secret := s.integ.SlackOAuthClient()
	tok, err := s.client.OpenIDToken(ctx, clientID, secret, p.Code, s.integ.SlackOAuthRedirectURL())
	if err != nil {
		log.Warn().Err(err).Msg("slack: Sign in with Slack token exchange failed")
		return ErrSlackVerifyFailed
	}
	claims, err := parseIDToken(tok)
	if err != nil || !claims.valid(clientID, st.Nonce, time.Now()) {
		return ErrSlackVerifyFailed
	}
	if claims.TeamID != c.SlackTeamID || claims.UserID != c.SlackUserID {
		return ErrSlackVerifyWrongAccount
	}
	return nil
}

type idTokenClaims struct {
	Iss    string   `json:"iss"`
	Aud    audience `json:"aud"`
	Exp    int64    `json:"exp"`
	Nonce  string   `json:"nonce"`
	TeamID string   `json:"https://slack.com/team_id"`
	UserID string   `json:"https://slack.com/user_id"`
}

// audience is an id_token aud claim, a string or a list of them.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

// parseIDToken reads an id_token's claims. The signature is not checked: the
// token comes straight from Slack's token endpoint over TLS, in exchange for
// this app's client secret (OpenID Connect Core 3.1.3.7).
func parseIDToken(tok string) (*idTokenClaims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed id_token")
	}
	body, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, err
	}
	var c idTokenClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *idTokenClaims) valid(clientID, nonce string, now time.Time) bool {
	aud := false
	for _, v := range c.Aud {
		aud = aud || (v == clientID && v != "")
	}
	return aud && c.Iss == oidcIssuer && nonce != "" && c.Nonce == nonce &&
		c.Exp > now.Unix() && c.TeamID != "" && c.UserID != ""
}
