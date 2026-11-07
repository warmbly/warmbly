package models

import (
	"testing"
	"time"
)

func TestMailTransientBlip(t *testing.T) {
	now := time.Date(2026, 11, 1, 6, 45, 0, 0, time.UTC)
	recent := now.Add(-20 * time.Minute)
	stale := now.Add(-MailTransientErrorGrace - time.Minute)

	cases := []struct {
		name     string
		severity string
		code     string
		op       MailTransportOperation
		source   MailTransportObservationSource
		success  map[MailTransportOperation]time.Time
		want     bool
	}{
		{"sync failure on a mailbox that synced recently", "WARNING", "SERVER_UNREACHABLE", MailTransportSync, MailTransportClockWorker, map[MailTransportOperation]time.Time{MailTransportSync: recent}, true},
		{"sync failure after a long outage", "WARNING", "SERVER_UNREACHABLE", MailTransportSync, MailTransportClockWorker, map[MailTransportOperation]time.Time{MailTransportSync: stale}, false},
		{"sync failure on a mailbox that never synced", "WARNING", "CONNECTION_LOST", MailTransportSync, MailTransportClockWorker, nil, false},
		{"send success does not excuse a sync failure", "WARNING", "SERVER_UNREACHABLE", MailTransportSync, MailTransportClockWorker, map[MailTransportOperation]time.Time{MailTransportSend: recent}, false},
		{"unnamed operation needs both to work", "WARNING", "SERVER_UNREACHABLE", MailTransportUnknown, MailTransportClockUnknown, map[MailTransportOperation]time.Time{MailTransportSync: recent}, false},
		{"unnamed operation with both working", "WARNING", "SERVER_UNREACHABLE", MailTransportUnknown, MailTransportClockUnknown, map[MailTransportOperation]time.Time{MailTransportSync: recent, MailTransportSend: recent}, true},
		{"critical report always shows", "CRITICAL", "SERVER_UNREACHABLE", MailTransportSync, MailTransportClockWorker, map[MailTransportOperation]time.Time{MailTransportSync: recent}, false},
		{"sign-in failure always shows", "WARNING", "AUTHENTICATION_FAILED", MailTransportSync, MailTransportClockWorker, map[MailTransportOperation]time.Time{MailTransportSync: recent}, false},
	}
	for _, c := range cases {
		if got := MailTransientBlip(c.severity, c.code, c.op, c.source, c.success, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
