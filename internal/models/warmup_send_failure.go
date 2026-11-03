package models

import "time"

const (
	MailboxNotLoadedPrefix = "The sending worker has not loaded this mailbox yet"
	MailboxLoadingCode     = "MAILBOX_LOADING"
	// ServerPacingCode is a send the worker held, or the server refused before any message was offered, because the server is limiting the worker's address.
	ServerPacingCode         = "SERVER_PACING"
	WarmupFailureLoading     = "mailbox_loading"
	WarmupLoadingGracePeriod = time.Hour
)
