package goog

import (
	"context"
	"fmt"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// SendAs is one address the account may send as, as Gmail reports it. Kept as
// the provider's shape so the caller decides what to keep: the worker relays
// identities and one signature, not every signature on the account.
type SendAs struct {
	Email              string
	Name               string
	Signature          string
	IsPrimary          bool
	IsDefault          bool
	VerificationStatus string
}

// Verified reports whether Gmail will accept mail From this address. The
// primary address carries no verificationStatus at all (it cannot be
// unverified), so an absent one is not a failed verification.
func (s SendAs) Verified() bool {
	return s.IsPrimary || s.VerificationStatus == "accepted"
}

// ListSendAs reads the mailbox's send-as identities, the gmail.settings.basic
// half of the sending identity.
//
// This runs on the worker holding the mailbox, like every other call against a
// customer's provider: the address Google sees for a mailbox is what drives
// its sign-in risk checks, and a mailbox whose settings are read from the
// control plane while its mail moves through a worker looks like two clients.
func (c *Client) ListSendAs(ctx context.Context) ([]SendAs, error) {
	if c.srv == nil {
		return nil, fmt.Errorf("gmail service not initialized")
	}

	resp, err := c.srv.Users.Settings.SendAs.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list send-as addresses: %w", err)
	}

	out := make([]SendAs, 0, len(resp.SendAs))
	for _, s := range resp.SendAs {
		if s == nil {
			continue
		}
		out = append(out, SendAs{
			Email:              s.SendAsEmail,
			Name:               s.DisplayName,
			Signature:          s.Signature,
			IsPrimary:          s.IsPrimary,
			IsDefault:          s.IsDefault,
			VerificationStatus: s.VerificationStatus,
		})
	}
	return out, nil
}

// SendAsIdentities narrows the provider's rows to what Warmbly stores.
// Addresses are lowercased because that is what every comparison against them
// does, and a blank row is dropped rather than stored as an empty identity.
func SendAsIdentities(rows []SendAs) []models.SendAsIdentity {
	out := make([]models.SendAsIdentity, 0, len(rows))
	for _, r := range rows {
		addr := strings.ToLower(strings.TrimSpace(r.Email))
		if addr == "" {
			continue
		}
		out = append(out, models.SendAsIdentity{
			Email:     addr,
			Name:      strings.TrimSpace(r.Name),
			IsPrimary: r.IsPrimary,
			IsDefault: r.IsDefault,
			Verified:  r.Verified(),
		})
	}
	return out
}

// SignatureFor returns the signature belonging to the address the mailbox
// sends as, falling back to the provider's default and then to the primary. A
// mailbox sending as an alias signs off as that alias, which is the whole
// point of having picked one.
func SignatureFor(rows []SendAs, sendAs string) string {
	want := strings.ToLower(strings.TrimSpace(sendAs))
	var chosen *SendAs
	for i := range rows {
		r := &rows[i]
		if want != "" && strings.ToLower(strings.TrimSpace(r.Email)) == want {
			chosen = r
			break
		}
		if chosen == nil && (r.IsDefault || r.IsPrimary) {
			chosen = r
		}
	}
	if chosen == nil {
		return ""
	}
	return strings.TrimSpace(chosen.Signature)
}
