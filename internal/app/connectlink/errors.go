package connectlink

import (
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Refusals a client sees on the connect page. Each carries a stable code the page maps to its own copy.
var (
	ErrNotFound = errx.NewWithIdentifier(errx.NotFound, "client_link_not_found",
		"This link does not exist. Check the address you were sent.")
	ErrExpired = errx.NewWithIdentifier(errx.Forbidden, "client_link_expired",
		"This link has expired. Ask whoever sent it for a new one.")
	ErrRevoked = errx.NewWithIdentifier(errx.Forbidden, "client_link_revoked",
		"This link was turned off. Ask whoever sent it for a new one.")
	ErrExhausted = errx.NewWithIdentifier(errx.Conflict, "client_link_exhausted",
		"This link has already connected every mailbox it allows. Ask whoever sent it for a new one.")
	ErrProviderNotAllowed = errx.NewWithIdentifier(errx.BadRequest, "client_link_provider_not_allowed",
		"This link does not accept that kind of mailbox.")
	ErrProviderUnavailable = errx.NewWithIdentifier(errx.BadRequest, "client_link_provider_unavailable",
		"That sign-in is not available here. Connect the mailbox with its server details instead.")
	ErrSigninExpired = errx.NewWithIdentifier(errx.BadRequest, "client_link_signin_expired",
		"This sign-in expired or was already used. Start again from the link.")
	ErrSigninDeclined = errx.NewWithIdentifier(errx.BadRequest, "client_link_signin_declined",
		"The sign-in was cancelled, so nothing was connected.")
	ErrCaptcha = errx.NewWithIdentifier(errx.BadRequest, "client_link_captcha_failed",
		"We could not verify you are human. Complete the security check and try again.")
	ErrRateLimited = errx.NewWithIdentifier(errx.TooManyRequests, "client_link_rate_limited",
		"Too many attempts through this link. Wait a few minutes and try again.")
	ErrUnavailable = errx.NewWithIdentifier(errx.ServiceUnavailable, "client_links_unavailable",
		"Client connect links need the portal service. Set PORTAL_DOMAIN on this instance and run the portal service.")
	ErrInvalidProviders = errx.NewWithIdentifier(errx.BadRequest, "client_link_invalid_providers",
		"Choose at least one of gmail, outlook, smtp_imap and vendor.")
	ErrInvalidMax = errx.NewWithIdentifier(errx.BadRequest, "client_link_invalid_max_mailboxes",
		"max_mailboxes must be between 1 and 500.")
	ErrInvalidExpiry = errx.NewWithIdentifier(errx.BadRequest, "client_link_invalid_expiry",
		"expires_in_days must be between 1 and 90.")
	ErrNoMailboxes = errx.NewWithIdentifier(errx.BadRequest, "client_link_no_mailboxes",
		"Pick at least one mailbox to connect.")
	ErrTooManyMailboxes = errx.NewWithIdentifier(errx.Conflict, "client_link_too_many_mailboxes",
		"This link cannot connect that many mailboxes. Pick fewer, or ask whoever sent it for a link that takes more.")
	ErrTooManyTags = errx.NewWithIdentifier(errx.BadRequest, "client_link_invalid_tags",
		"A link can apply at most 20 tags.")
)

// statusError is the refusal for a link that is not active, nil when it is.
func statusError(s models.ClientConnectLinkStatus) *errx.Error {
	switch s {
	case models.ClientConnectLinkExpired:
		return ErrExpired
	case models.ClientConnectLinkRevoked:
		return ErrRevoked
	case models.ClientConnectLinkExhausted:
		return ErrExhausted
	}
	return nil
}
