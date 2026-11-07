package models

import "time"

type MailTransportOperation string

type MailTransportObservationSource string

const (
	MailTransportClockUnknown MailTransportObservationSource = "unknown"
	MailTransportClockWorker  MailTransportObservationSource = "worker"
	MailTransportClockReceipt MailTransportObservationSource = "receipt"
)

const (
	MailTransportUnknown MailTransportOperation = "unknown"
	MailTransportSync    MailTransportOperation = "sync"
	MailTransportSend    MailTransportOperation = "send"
)

func (o MailTransportOperation) Known() bool {
	return o == MailTransportSync || o == MailTransportSend
}

func (o MailTransportOperation) Conservative() MailTransportOperation {
	if o.Known() {
		return o
	}
	return MailTransportUnknown
}

// Recovery needs an actual operation clock, not a synthesized receipt clock.
func ValidMailTransportTime(at, received time.Time) bool {
	return !at.IsZero() && at.Unix() > 0 && !at.After(received)
}

// MailTransientErrorGrace is how long after its last success a sync or send keeps its temporary failures out of the customer's view.
const MailTransientErrorGrace = 2 * time.Hour

// MailTransientErrorCodes are the reports a later successful operation disproves on its own.
var MailTransientErrorCodes = []string{"SERVER_UNREACHABLE", "CONNECTION_LOST", "RESOURCE_NOT_FOUND", "IMAP_UNKNOWN", "SENDING_TOO_FAST", "QUOTA_EXCEEDED"}

func IsMailTransientErrorCode(code string) bool {
	for _, c := range MailTransientErrorCodes {
		if c == code {
			return true
		}
	}
	return false
}

// MailTransientBlip reports whether a temporary warning sits on a mailbox whose affected operation still succeeded within the grace window.
// A report that does not name its operation needs both sync and send to have succeeded.
func MailTransientBlip(severity, code string, op MailTransportOperation, source MailTransportObservationSource, lastSuccess map[MailTransportOperation]time.Time, now time.Time) bool {
	if severity != "WARNING" || !IsMailTransientErrorCode(code) {
		return false
	}
	needed := []MailTransportOperation{op}
	if !op.Known() || source == MailTransportClockUnknown {
		needed = []MailTransportOperation{MailTransportSync, MailTransportSend}
	}
	for _, need := range needed {
		at, ok := lastSuccess[need]
		if !ok || now.Sub(at) > MailTransientErrorGrace {
			return false
		}
	}
	return true
}
