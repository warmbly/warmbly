package aitools

import "errors"

var (
	// ErrToolNotFound is returned by Registry.Call for an unknown tool name.
	ErrToolNotFound = errors.New("tool not found")
	// ErrToolForbidden is returned when the invocation lacks the tool's
	// required permission.
	ErrToolForbidden = errors.New("tool not permitted")
	// ErrInvalidArgs is returned by a handler when the model's JSON arguments
	// fail to decode or validate. It is fed back to the model so it can retry.
	ErrInvalidArgs = errors.New("invalid tool arguments")
	// errUniboxNotEntitled mirrors the HTTP unibox 403 when the org has no
	// active trial/paid plan.
	errUniboxNotEntitled = errors.New("the unified inbox requires an active trial or paid subscription")
)

// errRecipientSuppressed reports a refused send to a suppressed recipient
// (bounced/complained/unsubscribed), mirroring the compose handler's 400. The
// message is fed back to the model so it can explain why it did not send.
func errRecipientSuppressed(msg string) error {
	return errors.New(msg)
}
