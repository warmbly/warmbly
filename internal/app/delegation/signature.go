package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
)

var (
	// ErrSignatureRefused is Gmail refusing the signature or the address itself, which a retry will not change.
	ErrSignatureRefused = errors.New("delegation: gmail refused the signature")
	// ErrSignatureUnavailable is a mailbox this instance cannot mint a Gmail token for right now.
	ErrSignatureUnavailable = errors.New("delegation: the mailbox's gmail settings cannot be reached")
)

// SetPurchasedSignature sets the Gmail signature on a purchased Google mailbox's own address, with the
// store's brokered token or, for a Workspace delegated to this instance, its service account.
func (s *Service) SetPurchasedSignature(ctx context.Context, orgID uuid.UUID, address, signatureHTML string, brokered bool) error {
	address = strings.ToLower(strings.TrimSpace(address))
	var ts oauth2.TokenSource
	switch {
	case brokered:
		if s.purchased == nil {
			return ErrSignatureUnavailable
		}
		// The development simulator's tokens are opaque; Google is never called with them.
		if s.purchased.Simulated() {
			return nil
		}
		ts = s.purchasedGoogleSource(orgID, address, gmail.GmailSettingsBasicScope)
	case s.googleEnabled():
		var err error
		if ts, err = s.googleSource(address, gmail.GmailSettingsBasicScope); err != nil {
			return err
		}
	default:
		return ErrSignatureUnavailable
	}
	err := s.patchJSON(ctx, ts, s.gmailBase+"/users/me/settings/sendAs/"+url.PathEscape(address), map[string]string{"signature": signatureHTML})
	var ae *apiError
	if errors.As(err, &ae) && (ae.status == http.StatusBadRequest || ae.status == http.StatusNotFound) {
		return ErrSignatureRefused
	}
	return err
}

// patchJSON sends a JSON PATCH to a Google endpoint with a token from ts.
func (s *Service) patchJSON(ctx context.Context, ts oauth2.TokenSource, u string, body any) error {
	tok, err := ts.Token()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &apiError{status: resp.StatusCode, body: string(msg)}
	}
	return nil
}
