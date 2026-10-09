package wmail

import (
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

func (w *WMail) onTokenUpdate(token *oauth2.Token) error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.stopped {
		return nil
	}
	if data := w.executionData; data != nil && !data.Brokered {
		previous := ExecutionKey(data)
		if data.Google != nil {
			data.Google.Token = token
		}
		if data.Graph != nil {
			data.Graph.Token = token
		}
		w.executionKeys = map[[32]byte]struct{}{w.initialExecutionKey: {}, previous: {}, ExecutionKey(data): {}}
	}
	return w.onEvent(models.JobEventTypeTokenUpdate, &models.JobEventTokenUpdate{
		UserID:       w.UserID,
		EmailID:      w.ID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
	})
}
