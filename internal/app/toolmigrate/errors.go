package toolmigrate

import "github.com/warmbly/warmbly/internal/errx"

// Stable error codes, documented in api/error-codes.mdx.
const (
	CodeProviderUnknown     = "tool_migration_provider_unknown"
	CodeKeyRejected         = "tool_migration_key_rejected"
	CodeProviderUnavailable = "tool_migration_provider_unavailable"
	CodeNotReady            = "tool_migration_not_ready"
	CodeRunning             = "tool_migration_running"
	CodeTargetInvalid       = "tool_migration_target_invalid"
	CodePermission          = "tool_migration_permission"
	CodeInFlightUnsupported = "tool_migration_in_flight_unsupported"
	CodeTooMany             = "tool_migration_limit"
)

var (
	errNotFound        = errx.NewWithIdentifier(errx.NotFound, "tool_migration_not_found", "That migration does not exist.")
	errProviderUnknown = errx.NewWithIdentifier(errx.BadRequest, CodeProviderUnknown, "Warmbly cannot migrate from that tool.")
	errNotReady        = errx.NewWithIdentifier(errx.Conflict, CodeNotReady, "This migration is not ready to start. Wait for the scan to finish, or start a new one.")
	errRunning         = errx.NewWithIdentifier(errx.Conflict, CodeRunning, "This migration is still running. Cancel it first.")
	errInFlight        = errx.NewWithIdentifier(errx.BadRequest, CodeInFlightUnsupported,
		"Continuing leads part way through a sequence is not available yet. Choose restart to start them from the first email, or skip to import them without enrolling.")
	errScope      = errx.New(errx.BadRequest, "a migration belongs to exactly one workspace or agency")
	errRestricted = errx.NewWithIdentifier(errx.Forbidden, CodePermission, "A migration writes across the whole workspace, so it needs access to all of it, not only granted mailboxes or campaigns.")
)

func errPermission(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.Forbidden, CodePermission, msg)
}

func errTarget(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, CodeTargetInvalid, msg)
}
