package wmail

import (
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

func (w *WMail) onTokenUpdate(token *oauth2.Token) error {
	return w.onEvent(models.JobEventTypeTokenUpdate, &models.JobEventTokenUpdate{
		UserID:       w.UserID,
		EmailID:      w.ID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
	})
}
