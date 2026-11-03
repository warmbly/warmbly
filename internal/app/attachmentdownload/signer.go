package attachmentdownload

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"
)

const Lifetime = 15 * time.Minute
const payloadSize = 3*16 + 8

var ErrInvalid = errors.New("attachment download unavailable")

type Claims struct {
	OrganizationID uuid.UUID
	CampaignID     uuid.UUID
	AttachmentID   uuid.UUID
}

type Signer struct{ key []byte }

func New(secret string) *Signer {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("warmbly:campaign-attachment-download:v1"))
	return &Signer{key: mac.Sum(nil)}
}

func (s *Signer) Token(claims Claims, now time.Time) string {
	if s == nil {
		return ""
	}
	raw := make([]byte, 0, payloadSize+sha256.Size)
	raw = append(raw, claims.OrganizationID[:]...)
	raw = append(raw, claims.CampaignID[:]...)
	raw = append(raw, claims.AttachmentID[:]...)
	raw = binary.BigEndian.AppendUint64(raw, uint64(now.Add(Lifetime).Unix()))
	mac := hmac.New(sha256.New, s.key)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...))
}

func (s *Signer) Verify(token string, now time.Time) (Claims, error) {
	var claims Claims
	if s == nil || len(token) != base64.RawURLEncoding.EncodedLen(payloadSize+sha256.Size) {
		return claims, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != payloadSize+sha256.Size {
		return claims, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(raw[:payloadSize])
	if !hmac.Equal(raw[payloadSize:], mac.Sum(nil)) || int64(binary.BigEndian.Uint64(raw[48:56])) <= now.Unix() {
		return claims, ErrInvalid
	}
	copy(claims.OrganizationID[:], raw[:16])
	copy(claims.CampaignID[:], raw[16:32])
	copy(claims.AttachmentID[:], raw[32:48])
	if claims.OrganizationID == uuid.Nil || claims.CampaignID == uuid.Nil || claims.AttachmentID == uuid.Nil {
		return Claims{}, ErrInvalid
	}
	return claims, nil
}
