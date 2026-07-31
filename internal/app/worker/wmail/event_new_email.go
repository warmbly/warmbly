package wmail

import "github.com/warmbly/warmbly/internal/models"

func (w *WMail) onNewEmailEvent(data *models.EmailMessageStoreData) error {
	return w.onEvent(models.JobEventTypeNewEmail, &models.JobEventNewEmail{
		UserID:  w.UserID,
		Message: data,
	})
}
