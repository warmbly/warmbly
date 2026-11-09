package models

import (
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
)

func TestWarmupSendFailureKind(t *testing.T) {
	for message, want := range map[string]string{
		MailboxNotLoadedPrefix + ". It is reloaded automatically.": WarmupFailureLoading,
		errx.ErrMailSendingTooFast.Message:                         WarmupFailureThrottled,
		errx.ErrMailQuotaExceeded.Message:                          "",
		"Microsoft rejected the expired sign-in.":                  "",
		"": "",
	} {
		if got := WarmupSendFailureKind(message); got != want {
			t.Errorf("WarmupSendFailureKind(%q) = %q, want %q", message, got, want)
		}
	}
}
