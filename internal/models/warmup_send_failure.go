package models

import "time"

const (
	MailboxNotLoadedPrefix   = "The sending worker has not loaded this mailbox yet"
	WarmupFailureLoading     = "mailbox_loading"
	WarmupLoadingGracePeriod = time.Hour
)
