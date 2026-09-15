// Package unsublink mints and verifies the opaque tokens behind recipient
// unsubscribe links. A token names the organization, campaign and contact it
// was minted for and carries its own expiry, all under an HMAC, so a link is
// only ever honoured for the recipient it was sent to and nothing about it can
// be guessed or altered.
package unsublink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Validity is how long a minted link keeps working. CASL wants an opt-out to
// work for 60 days and CAN-SPAM for 30, so a year keeps a follow-up sent
// months into a sequence honourable long after the campaign ended.
const Validity = 365 * 24 * time.Hour

const (
	rawLen = 16 + 16 + 16 + 8
	macLen = 16
)

var (
	ErrInvalid = errors.New("unsubscribe token is invalid")
	ErrExpired = errors.New("unsubscribe token has expired")
)

// Claims is what a verified token says.
type Claims struct {
	OrgID      uuid.UUID
	CampaignID uuid.UUID
	ContactID  uuid.UUID
	ExpiresAt  time.Time
}

// Path is the path every minted link carries, on whichever origin it is
// served from. Click tracking recognises opt-out links by this segment, so it
// is the same on the API origin and on a workspace's own tracking domain.
const Path = "/unsubscribe/"

// Signer mints links under a key derived from the instance auth secret. The
// key is scoped with a purpose string so an unsubscribe token can never be
// replayed as any other signed artefact that shares the secret.
type Signer struct {
	key     []byte
	baseURL string
}

// New builds a signer. baseURL is the public origin of the API (the process
// that serves /unsubscribe); empty means links cannot be minted and Enabled
// reports false.
func New(secret, baseURL string) *Signer {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("warmbly:unsubscribe-link:v1"))
	return &Signer{key: mac.Sum(nil), baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/")}
}

// Enabled reports whether the signer knows a public origin to mint links on.
func (s *Signer) Enabled() bool {
	return s != nil && s.baseURL != "" && len(s.key) > 0
}

// Token mints the bare token for the given recipient.
func (s *Signer) Token(orgID, campaignID, contactID uuid.UUID, now time.Time) string {
	raw := make([]byte, 0, rawLen+macLen)
	raw = append(raw, orgID[:]...)
	raw = append(raw, campaignID[:]...)
	raw = append(raw, contactID[:]...)
	raw = binary.BigEndian.AppendUint64(raw, uint64(now.Add(Validity).Unix()))
	raw = append(raw, s.mac(raw)...)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// URL mints the full link for the given recipient on the API origin, or ""
// when disabled.
func (s *Signer) URL(orgID, campaignID, contactID uuid.UUID, now time.Time) string {
	return s.URLOn("", orgID, campaignID, contactID, now)
}

// URLOn mints the link on a specific origin: the workspace's own verified
// tracking domain, so the address a recipient reads sits on the sender's
// domain rather than the platform's. An empty origin (no custom domain, or a
// deployment that serves opt-outs from the API only) falls back to the API
// origin, which always serves the same routes.
func (s *Signer) URLOn(origin string, orgID, campaignID, contactID uuid.UUID, now time.Time) string {
	return s.urlFor(origin, s.Token(orgID, campaignID, contactID, now))
}

// TicketURL is the address a stored short ticket is read at, on the same
// origins and the same path as a signed one, so nothing downstream (the
// tracking-domain proxy, the click-tracking skip) has to know which it got.
func (s *Signer) TicketURL(origin, token string) string {
	return s.urlFor(origin, token)
}

// urlFor puts a token on an origin, falling back to the API origin for a
// workspace with no verified tracking domain of its own.
func (s *Signer) urlFor(origin, token string) string {
	if !s.Enabled() || token == "" {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(origin), "/")
	if base == "" {
		base = s.baseURL
	}
	return base + Path + token
}

// Verify checks the token's signature and expiry and returns its claims.
func (s *Signer) Verify(token string, now time.Time) (Claims, error) {
	if s == nil || len(s.key) == 0 {
		return Claims{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil || len(raw) != rawLen+macLen {
		return Claims{}, ErrInvalid
	}
	body, sig := raw[:rawLen], raw[rawLen:]
	if !hmac.Equal(sig, s.mac(body)) {
		return Claims{}, ErrInvalid
	}
	var c Claims
	copy(c.OrgID[:], body[0:16])
	copy(c.CampaignID[:], body[16:32])
	copy(c.ContactID[:], body[32:48])
	c.ExpiresAt = time.Unix(int64(binary.BigEndian.Uint64(body[48:56])), 0).UTC()
	if !now.Before(c.ExpiresAt) {
		return c, ErrExpired
	}
	return c, nil
}

func (s *Signer) mac(body []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write(body)
	return m.Sum(nil)[:macLen]
}
