package attachmentdownload

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDownloadCapabilityScopeIntegrityAndExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	signer := New("test-only-instance-key-with-at-least-32-bytes")
	claims := Claims{OrganizationID: uuid.New(), CampaignID: uuid.New(), AttachmentID: uuid.New()}
	token := signer.Token(claims, now)
	for _, at := range []time.Time{now, now.Add(Lifetime - time.Second)} {
		got, err := signer.Verify(token, at)
		if err != nil || got != claims {
			t.Fatalf("verify = %+v, %v", got, err)
		}
	}
	for _, at := range []time.Time{now.Add(Lifetime), now.Add(Lifetime + time.Second)} {
		if _, err := signer.Verify(token, at); err != ErrInvalid {
			t.Fatalf("expiry answered %v", err)
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{0, 16, 32, 48, payloadSize} {
		changed := append([]byte(nil), raw...)
		changed[offset] ^= 1
		if _, err := signer.Verify(base64.RawURLEncoding.EncodeToString(changed), now); err != ErrInvalid {
			t.Fatalf("altered field at %d answered %v", offset, err)
		}
	}
	for _, invalid := range []string{"", token[:len(token)-1], strings.Repeat("!", len(token))} {
		if _, err := signer.Verify(invalid, now); err != ErrInvalid {
			t.Fatalf("malformed capability answered %v", err)
		}
	}
	if _, err := New("another-test-only-instance-key-of-32-bytes").Verify(token, now); err != ErrInvalid {
		t.Fatalf("different instance key answered %v", err)
	}
	if _, err := signer.Verify(signer.Token(Claims{}, now), now); err != ErrInvalid {
		t.Fatalf("empty scope answered %v", err)
	}
	var disabled *Signer
	if disabled.Token(claims, now) != "" {
		t.Fatal("disabled signer issued a capability")
	}
	if _, err := disabled.Verify(token, now); err != ErrInvalid {
		t.Fatalf("disabled signer answered %v", err)
	}
}
