package errx

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/config"
)

var (
	ErrInvalid = New(BadRequest, "The request body contains invalid or malformed JSON.")

	ErrLock      = New(BadRequest, "Resource is locked. Another process is running, please wait.")
	ErrColor     = New(BadRequest, "Hex color must be a valid string.")
	ErrUuid      = New(BadRequest, "The id must be a valid uuid.")
	ErrCategory  = New(BadRequest, "Category doesn't exists.")
	ErrTag       = New(BadRequest, "Tag doesn't exists.")
	ErrBitmask   = New(BadRequest, "Invalid bitmask value.")
	ErrRole      = New(BadRequest, "Role doesn't exists.")
	ErrPosition  = New(BadRequest, "Invalid position.")
	ErrTimezone  = New(BadRequest, "Invalid timezone. Use an IANA name such as America/New_York or Europe/London.")
	ErrTime      = New(BadRequest, "Invalid time format.")
	ErrNotEnough = New(BadRequest, "Not enough data to perform this action.")
	ErrLimit     = New(BadRequest, "Limit must be between 10 and 200.")

	// Authorization
	ErrToken = New(Unauthorized, "Invalid or expired token.")
	ErrAuth  = New(Unauthorized, "Missing or invalid Authorization header.")
	// ErrSessionEnded answers a token whose session was signed out or reached
	// its absolute lifetime; only a new sign-in continues.
	ErrSessionEnded = NewWithIdentifier(Unauthorized, "session_ended", "Your session has ended. Sign in again to continue.")

	ErrUser     = New(BadRequest, "User doesn't exists.")
	ErrPassword = New(BadRequest, "Password must be at least 8 characters long.")
	// ErrPasswordTooLong and ErrPasswordBreached separate the two other ways a
	// password is refused, so nobody is told to lengthen a 40-character
	// passphrase that was rejected for appearing in a breach corpus.
	ErrPasswordTooLong  = New(BadRequest, "Password must be at most 128 characters long.")
	ErrPasswordBreached = NewWithIdentifier(BadRequest, "password_breached", "This password appears in a public list of breached passwords. Choose one that does not.")
	ErrEmail            = New(BadRequest, "Invalid email address.")
	ErrCredentials      = New(BadRequest, "Invalid email or password.")
	ErrSession          = New(BadRequest, "Invalid or expired session.")
	ErrCodeLimit        = New(BadRequest, "Too many attempts. Start a new session and try again later.")
	ErrCode             = New(BadRequest, "Invalid or expired verification code.")
	ErrAuthLimit        = New(BadRequest, "Too many attempts, please try again later.")

	// ErrMailUndeliverable separates "we could not send you the email" from
	// every other internal fault. It used to be a bare 500, which on a
	// self-hosted install with no working relay is the single least helpful
	// thing to show someone who cannot log in.
	// Public: this is the one internal fault whose message the person reading
	// it can act on, and blanking it leaves a self-hoster with a broken relay
	// no way to tell that is the problem.
	ErrMailUndeliverable = NewPublic(Internal, "We couldn't send the email. If you administer this server, check the mail transport configuration.")

	// Registration refusals. Each names the deployment policy rather than the
	// person, and carries its own identifier so a client can branch on the
	// specific condition instead of string-matching a 403.
	ErrRegistrationInviteOnly = NewWithIdentifier(Forbidden, "registration_invite_only",
		"This server is invite only. Ask an administrator to invite you, then open the link in the invitation to create your account. See https://docs.warmbly.com/development/accounts-and-access/")
	// ErrRegistrationClosed is returned when signups are off entirely. The
	// operator sets DISABLE_REGISTRATION=true, and no invitation overrides it.
	ErrRegistrationClosed = NewWithIdentifier(Forbidden, "registration_closed",
		"This server is not accepting new accounts. See https://docs.warmbly.com/development/accounts-and-access/")
	// ErrAccountExists answers a signup for an address that already has an account.
	ErrAccountExists = New(Conflict, "An account with this email address already exists. Sign in, or reset your password if you have forgotten it.")

	ErrInvitationInvalid = NewWithIdentifier(Forbidden, "invitation_invalid",
		"That invitation link is invalid, expired, or was issued for a different email address. Ask for a fresh one.")

	// First-run claim. Both name the command that resolves them, because the
	// person who sees these is the person who can run it.
	ErrSetupToken = NewWithIdentifier(Unauthorized, "setup_token_invalid",
		"That setup link is invalid, already used, or expired. Print a new one with `warmblyctl setup-link`.")
	ErrSetupComplete = NewWithIdentifier(Forbidden, "setup_already_complete",
		"This instance has already been set up. Sign in, or recover access with `warmblyctl user reset-password`.")

	// ErrSSOBrowser is a sign-in collected somewhere other than where it
	// started. The handoff is deliberately non-transferable, so a link someone
	// forwarded (or was sent) cannot sign the recipient into the sender's
	// workspace.
	ErrSSOBrowser = NewWithIdentifier(Unauthorized, "sso_wrong_browser",
		"Finish signing in in the browser you started in. Open the sign-in page there and try again.")

	// ErrSSOLinkExpired ends a link challenge: an unknown or expired pending
	// token, or too many wrong passwords. The client starts the sign-in over.
	ErrSSOLinkExpired = NewWithIdentifier(BadRequest, "sso_link_expired",
		"That sign-in request expired or had too many attempts. Start the sign-in again.")

	// ErrPasswordChangeRequired refuses using an issued initial password anywhere but the password sign-in, which replaces it.
	ErrPasswordChangeRequired = NewWithIdentifier(Forbidden, "password_change_required",
		"This account still has the password it was issued, which has to be replaced first. Sign in with your email and password to choose your own.")

	// ErrPasswordChangeExpired ends a choose-your-password step that is unknown, expired, already used, or whose issued password ran out.
	ErrPasswordChangeExpired = NewWithIdentifier(BadRequest, "password_change_expired",
		"That sign-in request expired or was already used. Sign in again.")

	// ErrPasswordReused refuses choosing the issued initial password as your own.
	ErrPasswordReused = NewWithIdentifier(BadRequest, "password_reused",
		"Choose a password of your own, different from the one you were given.")

	// Re-authentication through a linked identity.
	ErrReauthIdentityMismatch = NewWithIdentifier(BadRequest, "reauth_identity_mismatch",
		"That account is not the one linked to this Warmbly account. Continue with the account you sign in with.")
	ErrReauthExpired = NewWithIdentifier(BadRequest, "reauth_expired",
		"That confirmation expired or was already used. Try again.")

	ErrExternalCode     = New(BadRequest, "Invalid or expired code, please try again.")
	ErrExternalEmail    = New(BadRequest, "Invalid or unverified email address.")
	ErrExternalProvider = New(BadRequest, "This sign-in method isn't available on this server.")

	// Sessions
	ErrSessionNotFound = New(NotFound, "Session not found.")
	ErrSessionCurrent  = New(BadRequest, "You can't revoke the session you're currently using. Use sign out instead.")

	// Passkeys (WebAuthn)
	ErrPasskey         = New(BadRequest, "We couldn’t verify that passkey. Please try again.")
	ErrPasskeySession  = New(BadRequest, "Your passkey request expired. Please start again.")
	ErrPasskeyName     = New(BadRequest, "Passkey name must be between 1 and 60 characters.")
	ErrPasskeyNotFound = New(NotFound, "Passkey not found.")
	ErrPasskeyExists   = New(Conflict, "This passkey is already registered.")
	ErrPasskeyNone     = New(BadRequest, "No passkey was found for this account.")
	// A passkey sign-in counts as multi-factor, so the authenticator must verify the user.
	ErrPasskeyUserVerification = NewWithIdentifier(BadRequest, "passkey_user_verification_required", "This passkey didn’t ask for a PIN or biometric. Set a PIN on the security key, or sign in another way.")

	// Organization
	//
	// Raised where a workspace is required to scope a write or a policy check.
	// Every entitlement, limit and suppression rule is org-scoped, so a request
	// without one is refused rather than run unscoped.
	ErrNoOrganization = NewWithIdentifier(BadRequest, "no_organization",
		"No workspace selected. Pick a workspace and try again.")

	// Roles
	ErrRoleName = New(BadRequest, "Role name length must be between 3 and 100 characters.")

	// Captch
	ErrCaptcha = New(BadRequest, "We couldn’t verify you’re human. Please try the security check again or reload the page.")
	// The native app's proof in place of the captcha.
	ErrAppAttest        = NewWithIdentifier(BadRequest, "app_attest_failed", "We couldn’t verify this copy of the app. Update it from the App Store and try again.")
	ErrAppAttestUnknown = NewWithIdentifier(BadRequest, "app_attest_key_unknown", "This device needs to verify the app again.")

	// Group
	ErrGroupTitle = New(BadRequest, "The title length must be between 1 and 50 characters.")
	ErrGroupMax   = New(BadRequest, "You reached the maximum amount.")

	// Email
	ErrEmailCredentials = New(BadRequest, "Invalid email credentials.")
	// Raised when no worker reported back on a credential check at all. The
	// per-leg timeout, where a worker did answer and names the server that
	// stayed silent, is built in the email service under the same code.
	ErrEmailValidation = NewWithIdentifier(BadRequest, "mailbox_validation_timeout",
		"Warmbly's worker did not report back on this mailbox in time, so the mail server was not tested. Nothing was saved. Try again in a moment, and contact support if it keeps happening.")
	ErrEmailOnboardProvider = New(BadRequest, "Unsupported email provider. Use 'gmail', 'outlook', or 'smtp_imap'.")
	// Raised when the provider is supported but this deployment has no OAuth
	// client for it. Self-host only: the hosted product always has both set. The
	// message names the variables because the person hitting this is usually the
	// same person who can fix it.
	ErrEmailOnboardGoogleNotConfigured = NewWithIdentifier(ServiceUnavailable, "mailbox_provider_not_configured",
		"Gmail is not configured on this deployment. Set BOX_GOOGLE_CLIENT_ID and BOX_GOOGLE_CLIENT_SECRET in your .env, then restart. See https://docs.warmbly.com/development/deployment-guide/#connect-mailboxes")
	ErrEmailOnboardOutlookNotConfigured = NewWithIdentifier(ServiceUnavailable, "mailbox_provider_not_configured",
		"Microsoft 365 is not configured on this deployment. Set BOX_OUTLOOK_CLIENT_ID and BOX_OUTLOOK_CLIENT_SECRET in your .env, then restart. See https://docs.warmbly.com/development/deployment-guide/#connect-mailboxes")
	// Raised when a new Gmail mailbox asks for Google sign-in while the
	// deployment routes new Gmail mailboxes through an app password instead
	// (config.GoogleOAuthConnect). Re-authorizing an existing mailbox never
	// hits this.
	ErrEmailOnboardGoogleOAuthDisabled = NewWithIdentifier(Forbidden, "mailbox_gmail_oauth_disabled",
		"New Gmail mailboxes connect with an app password over IMAP and SMTP on this deployment, not with Google sign-in. Mailboxes already connected with Google sign-in keep working and can still be re-authorized. See https://docs.warmbly.com/guides/mailboxes/#gmail-and-google-workspace")
	ErrEmailOnboardState         = New(BadRequest, "Invalid or expired onboarding state.")
	ErrEmailOnboardReturnOrigin  = NewWithIdentifier(BadRequest, "mailbox_oauth_return_origin", "This dashboard origin is not allowed for mailbox OAuth. Add its exact origin to CORS_ALLOW_ORIGINS.")
	ErrEmailOnboardCode          = New(BadRequest, "Authorization code is missing or invalid.")
	ErrEmailOnboardExchange      = New(BadRequest, "Could not exchange the authorization code with the provider.")
	ErrEmailOnboardUserInfo      = New(BadRequest, "Could not read account details from the provider.")
	ErrEmailOnboardAlreadyExists = New(Conflict, "This email account is already connected.")
	ErrEmailOnboardNoWorker      = New(ServiceUnavailable, "No mailbox workers are available right now. Please try again shortly.")
	ErrEmailReauthProvider       = New(BadRequest, "This mailbox connects with SMTP/IMAP credentials. Update its credentials instead of re-authorizing.")
	ErrEmailReauthOAuthOnly      = New(BadRequest, "This mailbox signs in with OAuth. Re-authorize it instead of entering credentials.")
	ErrEmailReauthWrongAccount   = New(Conflict, "The account you signed in with is not this mailbox's address. Sign in with the mailbox's own account and try again.")
	ErrEmailReauthCloudManaged   = New(Conflict, "Warmbly Cloud holds this mailbox's sign-in. Reconnect it from your cloud workspace instead.")
	ErrEmailReauthDelegated      = NewWithIdentifier(Conflict, "mailbox_reauth_delegated", "An administrator's grant for this domain connects this mailbox, so it has nothing to sign in to. Check the grant from Add account > Google or Microsoft > Whole domain.")
	ErrEmailReauthNoRefreshToken = New(BadRequest, "The provider did not return a refresh token and none is stored. Please try re-authorizing again.")
	ErrEmailSMTPHost             = New(BadRequest, "SMTP host is required.")
	ErrEmailSMTPPort             = New(BadRequest, "SMTP port must be between 1 and 65535.")
	ErrEmailSMTPSecurity         = New(BadRequest, "SMTP security must be tls, starttls or none.")
	ErrEmailIMAPSecurity         = New(BadRequest, "IMAP security must be tls, starttls or none.")
	// The "none" refusals are two different problems with two different ways
	// out, so they do not share a message: one is a host that has to change,
	// the other is a mode this deployment cannot offer at all.
	ErrEmailSMTPSecurityNotLocal   = New(BadRequest, "SMTP security \"none\" is only allowed for a mail server on this machine (localhost, 127.0.0.1 or ::1).")
	ErrEmailIMAPSecurityNotLocal   = New(BadRequest, "IMAP security \"none\" is only allowed for a mail server on this machine (localhost, 127.0.0.1 or ::1).")
	ErrEmailSMTPSecurityHosted     = New(BadRequest, "SMTP security \"none\" needs a self-hosted instance, where the worker runs on the same machine as the mail server. Use tls or starttls.")
	ErrEmailIMAPSecurityHosted     = New(BadRequest, "IMAP security \"none\" needs a self-hosted instance, where the worker runs on the same machine as the mail server. Use tls or starttls.")
	ErrEmailSMTPTLSPin             = NewWithIdentifier(BadRequest, "invalid_tls_pin", "SMTP tls_pin must be a SHA-256 fingerprint: 64 hexadecimal characters.")
	ErrEmailIMAPTLSPin             = NewWithIdentifier(BadRequest, "invalid_tls_pin", "IMAP tls_pin must be a SHA-256 fingerprint: 64 hexadecimal characters.")
	ErrEmailCertificateNotPinned   = NewWithIdentifier(BadRequest, "mailbox_certificate_not_pinned", "Both servers on this mailbox are verified by a certificate authority, so there is no certificate to confirm.")
	ErrEmailCertificatePin         = NewWithIdentifier(BadRequest, "invalid_tls_pin", "Send the SHA-256 fingerprint of each server trusted on its own certificate (smtp_pin, imap_pin) as 64 hexadecimal characters. Run the certificate check first and confirm the fingerprints it reports.")
	ErrEmailCertificatePinMismatch = NewWithIdentifier(BadRequest, "mailbox_tls_failed", "The server did not present the certificate with the fingerprint you confirmed, so nothing was changed. Run the certificate check again (Re-confirm in the dashboard) and confirm the fingerprint it reports.")
	ErrEmailIMAPHost               = New(BadRequest, "IMAP host is required.")
	ErrEmailIMAPPort               = New(BadRequest, "IMAP port must be between 1 and 65535.")
	ErrEmailCredentialsRequired    = New(BadRequest, "SMTP and IMAP credentials are required.")
	ErrEmailTrackingDomain         = New(BadRequest, "Invalid tracking domain.")
	ErrEmailTrackingDomainLength   = New(BadRequest, "Tracking domain is too long (max 253 characters).")
	ErrEmailName                   = New(BadRequest, "Invalid name. Must be 2–100 characters and contain only letters, numbers, spaces, '-', '.', or '’'.")
	ErrEmailSignaturePlain         = New(BadRequest, "Plain email signature is too long.")
	ErrEmailSignatureHTML          = New(BadRequest, "HTML email signature is too long.")
	ErrEmailMinWaitTime            = New(BadRequest, "Minimum time gap between emails must be between 0 and 86400 seconds.")
	ErrEmailCampaignLimit          = New(BadRequest, fmt.Sprintf("Campaign limit must be between %d and %d.", config.LimitMin, config.LimitMax))
	ErrEmailTimezone               = New(BadRequest, "Invalid timezone. Use an IANA name such as Europe/London or America/Denver, or leave it empty to follow the campaign.")
	ErrEmailWarmupBase             = New(BadRequest, "Warmup base must be between 0 and 100.")
	ErrEmailWarmupMax              = New(BadRequest, "Warmup max amount must be between 0 and 100.")
	ErrEmailWarmupIncrease         = New(BadRequest, "Warmup increase amount must be between 0 and 100.")
	ErrEmailReplyRate              = New(BadRequest, "Warmup reply rate must be between 0 and 100.")
	ErrEmailWarmupPlacement        = New(BadRequest, "Warmup filing must be one of: folder, inbox, archive.")
	ErrEmailWarmupFolder           = New(BadRequest, fmt.Sprintf("Warmup folder must be at most %d characters and contain no folder separators or control characters.", config.WarmupFolderMaxLen))
	ErrEmailWarmupRetention        = New(BadRequest, fmt.Sprintf("Warmup retention must be between %d and %d days, or 0 to follow the instance setting.", config.WarmupMailRetentionDaysMin, config.RetentionDaysMax))

	// Disconnecting a mailbox has to reach the machine syncing it before the
	// row goes: afterwards there is no assignment left to read and nothing that
	// can repair a missed removal, so the mailbox would sync on forever.
	ErrEmailWorkerUnreachable = NewWithIdentifier(ServiceUnavailable, "mailbox_worker_unreachable",
		"This mailbox could not be disconnected right now because the machine syncing it could not be reached. Nothing was removed, so try again in a moment.")

	// Sending identity: the provider's own send-as list is the only authority
	// on which addresses a mailbox may use, so a choice outside it is refused
	// here rather than at send time, where the provider's refusal names
	// nothing the customer could act on.
	ErrEmailSendAsUnsupported = NewWithIdentifier(BadRequest, "mailbox_send_as_unsupported",
		"This mailbox's provider does not expose send-as addresses. Only Gmail and Google Workspace mailboxes do.")
	ErrEmailSendAsUnknown = NewWithIdentifier(BadRequest, "mailbox_send_as_unknown",
		"That address is not one your provider has verified this mailbox to send as. Refresh the list, or add and verify the address in your provider first.")
	// Reading a mailbox's sending identity is an account operation and runs on
	// the worker holding the mailbox, so it is unavailable exactly when that
	// machine is: mid-migration, just after a restart, or while the mailbox is
	// unplaced. Nothing was changed, and the next attempt is the fix.
	ErrEmailIdentityUnavailable = NewWithIdentifier(ServiceUnavailable, "mailbox_identity_unavailable",
		"Warmbly could not reach the machine running this mailbox, so its sending addresses were not refreshed. Nothing was changed; try again in a moment.")
	ErrEmailSignatureTooLarge = NewWithIdentifier(BadRequest, "mailbox_signature_too_large",
		fmt.Sprintf("The signature on this mailbox is larger than Warmbly stores (%d characters). Shorten it in your provider and import it again.", config.SignatureHTMLMax))

	// Campaign
	ErrCampaignName        = New(BadRequest, "Campaign name length must be between 3 and 50 characters.")
	ErrCampaignDescription = New(BadRequest, "Campaign description length must be below 300 characters.")
	ErrCampaignDailyLimit  = New(BadRequest, fmt.Sprintf("Daily limit must be between %d and %d.", config.CampaignDailyLimitMin, config.LimitMax))
	ErrCampaignStartDate   = New(BadRequest, "Start date cannot be in the past. Pick today or later, or clear it (null) to start right away.")
	ErrCampaignEndDate     = New(BadRequest, "End date must be in the future.")
	ErrCampaignLimit       = New(BadRequest, "You reached your limit for campaigns, please try again later.")

	// Sequence
	ErrSequenceName      = New(BadRequest, "Sequence name cannot be longer than 50 characters.")
	ErrSequenceSubject   = New(BadRequest, "Sequence subject cannot be longer than 100 characters.")
	ErrSequenceBody      = New(BadRequest, fmt.Sprintf("Sequence body cannot be longer than %d characters.", config.SequenceBodyLimit))
	ErrSequenceBranch    = New(BadRequest, "Invalid branching conditions.")
	ErrSequenceBranchTo  = New(BadRequest, "Branch target must be another step in the same campaign and cannot create a cycle.")
	ErrSequenceKind      = New(BadRequest, "Step kind must be email, action, or wait.")
	ErrSequenceAction    = New(BadRequest, "Invalid action configuration for this step.")
	ErrSequenceWaitAfter = New(BadRequest, fmt.Sprintf("Step wait must be between 0 and %d days.", config.SequenceWaitAfterMax))

	// Contact
	ErrContactSerialize = New(BadRequest, "Failed to serialize contact.")
	ErrContactSize      = New(BadRequest, "Contact size cannot be bigger than 10KB.")
	// A contact's address is unique within the workspace, so an edit that
	// collides with another contact is refused rather than merged: merging two
	// people's campaign progress is not something an edit can undo.
	ErrContactEmailTaken = NewWithIdentifier(Conflict, "contact_email_taken",
		"Another contact already uses this email address.")

	// Unibox
	ErrUniboxLimit = New(BadRequest, fmt.Sprintf("Limit must be between %d and %d.", config.UniboxLimitMin, config.UniboxLimitMax))
	ErrSeenMax     = New(BadRequest, "Cannot update more than 500 messages.")
	// Folder scoping (unibox sidebar).
	ErrUniboxFolder     = New(BadRequest, "Folder must be one of inbox, sent, drafts, archive, spam, trash.")
	ErrSeenFolderAndIDs = New(BadRequest, "Provide either email_ids or folder, not both.")
	// Filing a message is narrower than scoping a list: the other three are
	// verdicts the provider reaches, not somewhere a user puts mail.
	ErrUniboxFilableFolder = New(BadRequest, "Folder must be one of inbox, archive, trash.")

	// Servers
	ErrIPAddr    = New(BadRequest, "Invalid IP Address.")
	ErrPublicKey = New(BadRequest, "Invalid Public Key.")

	// Advisor
	ErrAdvisorNoAction     = New(BadRequest, "This recommendation doesn't have a one-click fix.")
	ErrAdvisorNotApplied   = New(BadRequest, "This recommendation hasn't been applied, so there's nothing to undo.")
	ErrAdvisorSnoozeRange  = New(BadRequest, "Snooze length must be between 1 and 90 days.")
	ErrAdvisorBadSeverity  = New(BadRequest, "Minimum severity must be one of critical, high, medium, or low.")
	ErrAdvisorFixForbidden = New(Forbidden, "You can see this recommendation but don't have permission to apply the change it makes.")
	ErrAdvisorNoAgentFix   = New(BadRequest, "This recommendation needs a person: there's no change an agent can safely make for it.")

	// ErrAttemptCheckUnavailable is answered when the per-account attempt
	// budget cannot be read or charged. The attempt is not evaluated, so the
	// caller can retry it safely once the cache answers again.
	ErrAttemptCheckUnavailable = &Error{Code: ServiceUnavailable, Identifier: "attempt_check_unavailable", RetryAfter: 5,
		Message: "We couldn't check this attempt right now, so it was not counted. Wait a few seconds and try again."}
	// ErrCodeLookupUnavailable is the same condition for a pairing-code lookup.
	ErrCodeLookupUnavailable = &Error{Code: ServiceUnavailable, Identifier: "attempt_check_unavailable", RetryAfter: 5,
		Message: "We couldn't look up this code right now. Wait a few seconds and try again."}

	// Account and workspace standing. Each names what the person can do next.
	ErrAccountSuspended = NewWithIdentifier(Forbidden, "account_suspended",
		"This account is suspended and cannot sign in. If you think this is a mistake, contact support or the administrator of this server.")
	ErrAccountSendingSuspended = NewWithIdentifier(Forbidden, "account_sending_suspended",
		"This account is not allowed to send email. If you think this is a mistake, contact support or the administrator of this server.")
	ErrAccountWorkspaceCreateSuspended = NewWithIdentifier(Forbidden, "account_workspace_create_suspended",
		"This account is not allowed to create new workspaces. You can still join a workspace you are invited to. If you think this is a mistake, contact support or the administrator of this server.")
	ErrWorkspaceSendingSuspended = NewWithIdentifier(Forbidden, "workspace_sending_suspended",
		"Sending is paused for this workspace while it is under review. Contact support to resolve the review; nothing else in the workspace is affected.")

	// Tracking domains
	ErrTrackingDomainTaken = NewWithIdentifier(Conflict, "tracking_domain_taken", "Another workspace on this instance already uses this tracking domain. Choose a host on a domain your workspace sends from.")

	// Forms domains
	ErrFormsDomainTaken = NewWithIdentifier(Conflict, "forms_domain_taken", "Another workspace on this instance already uses this domain. Choose a host on a domain your workspace owns.")
)

// MailboxAllowanceReached is the refusal every connect path returns when the
// workspace holds its whole allowance. The identifier is stable so the
// dashboard can open the request-more flow instead of showing the text.
func MailboxAllowanceReached(used, allowance int, paid bool) *Error {
	if !paid {
		return NewWithIdentifier(Forbidden, "mailbox_allowance_reached",
			fmt.Sprintf("A free workspace holds up to %d mailboxes. Choose a plan to add more.", allowance))
	}
	return NewWithIdentifier(Forbidden, "mailbox_allowance_reached",
		fmt.Sprintf("This workspace holds %d of its %d mailboxes. Request an increase, or move to a plan with more daily sends.", used, allowance))
}

// StorageLimitReached is the refusal for an upload or copy (an attachment, or
// an image for an email body) that would take the workspace past its storage
// quota.
func StorageLimitReached(usedBytes, limitBytes, addingBytes int64) *Error {
	const mb = 1024 * 1024
	return NewWithIdentifier(BadRequest, "storage_limit_reached",
		fmt.Sprintf("Storage limit reached: %d MB of %d MB used, %d MB to add. Remove attachments or images, or upgrade your plan.",
			usedBytes/mb, limitBytes/mb, addingBytes/mb))
}
