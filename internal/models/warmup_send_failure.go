package models

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

const (
	MailboxNotLoadedPrefix = "The sending worker has not loaded this mailbox yet"
	MailboxLoadingCode     = "MAILBOX_LOADING"
	// ServerPacingCode is a send the worker held, or the server refused before any message was offered, because the server is limiting the worker's address.
	ServerPacingCode = "SERVER_PACING"
	// SenderPausedCode is a send refused for the sending mailbox's own limit or restriction, never the recipient.
	SenderPausedCode     = "SENDER_PAUSED"
	WarmupFailureLoading = "mailbox_loading"
	// WarmupFailureThrottled is the provider pacing the mailbox, which the scheduler absorbs on its own.
	WarmupFailureThrottled   = "provider_throttle"
	WarmupLoadingGracePeriod = time.Hour
)

// WarmupSendFailureKind classifies a stored warmup failure message; empty is an ordinary failure.
func WarmupSendFailureKind(message string) string {
	switch {
	case strings.HasPrefix(message, MailboxNotLoadedPrefix):
		return WarmupFailureLoading
	case message == errx.ErrMailSendingTooFast.Message:
		return WarmupFailureThrottled
	}
	return ""
}
