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
	ErrTimezone  = New(BadRequest, "Timezone doesn't exists.")
	ErrTime      = New(BadRequest, "Invalid time format.")
	ErrNotEnough = New(BadRequest, "Not enough data to perform this action.")
	ErrJSONKey   = New(BadRequest, "JSON key can only contain characters and underscore.")
	ErrLimit     = New(BadRequest, "Limit must be between 10 and 200.")

	// Authorization
	ErrToken = New(Unauthorized, "Invalid or expired token.")
	ErrAuth  = New(Unauthorized, "Missing or invalid Authorization header.")

	ErrUser        = New(BadRequest, "User doesn't exists.")
	ErrPassword    = New(BadRequest, "Password must be at least 8 characters long.")
	ErrEmail       = New(BadRequest, "Invalid email address.")
	ErrCredentials = New(BadRequest, "Invalid email or password.")
	ErrSession     = New(BadRequest, "Invalid or expired session.")
	ErrCodeLimit   = New(BadRequest, "Too many attempts. Start a new session and try again later.")
	ErrCode        = New(BadRequest, "Invalid or expired verification code.")
	ErrAuthLimit   = New(BadRequest, "Too many attempts, please try again later.")

	ErrExternalCode  = New(BadRequest, "Invalid or expired code, please try again.")
	ErrExternalEmail = New(BadRequest, "Invalid or unverified email address.")

	// Roles
	ErrRoleName = New(BadRequest, "Role name length must be between 3 and 100 characters.")

	// Captch
	ErrCaptcha = New(BadRequest, "We couldn’t verify you’re human. Please try the security check again or reload the page.")

	// Group
	ErrGroupTitle = New(BadRequest, "The title length must be between 1 and 50 characters.")
	ErrGroupMax   = New(BadRequest, "You reached the maximum amount.")

	// Email
	ErrEmailCredentials          = New(BadRequest, "Invalid email credentials.")
	ErrEmailValidation           = New(BadRequest, "Deadline exceed, try again later.")
	ErrEmailTrackingDomain       = New(BadRequest, "Invalid tracking domain.")
	ErrEmailTrackingDomainLength = New(BadRequest, "Tracking domain is too long (max 253 characters).")
	ErrEmailName                 = New(BadRequest, "Invalid name. Must be 2–100 characters and contain only letters, numbers, spaces, '-', '.', or '’'.")
	ErrEmailSignaturePlain       = New(BadRequest, "Plain email signature is too long.")
	ErrEmailSignatureHTML        = New(BadRequest, "HTML email signature is too long.")
	ErrEmailMinWaitTime          = New(BadRequest, "Minimum time gap between emails must be between 0 and 86400 minutes.")
	ErrEmailCampaignLimit        = New(BadRequest, "Campaign limit must be between 0 and 100.")
	ErrEmailWarmupBase           = New(BadRequest, "Warmup base must be between 0 and 100.")
	ErrEmailWarmupMax            = New(BadRequest, "Warmup max amount must be between 0 and 100.")
	ErrEmailWarmupIncrease       = New(BadRequest, "Warmup increase amount must be between 0 and 100.")
	ErrEmailReplyRate            = New(BadRequest, "Warmup reply rate must be between 0 and 100.")

	// Campaign
	ErrCampaignName        = New(BadRequest, "Campaign name length must be between 3 and 50 characters.")
	ErrCampaignDescription = New(BadRequest, "Campaign description length must be below 300 characters.")
	ErrCampaignDailyLimit  = New(BadRequest, "Daily limit must be between 3 and 10000000.")
	ErrCampaignStartDate   = New(BadRequest, "Start date must be in the future, use null if you want to start now.")
	ErrCampaignEndDate     = New(BadRequest, "End date must be in the future.")
	ErrCampaignLimit       = New(BadRequest, "You reached your limit for campaigns, please try again later.")

	// Sequence
	ErrSequenceName    = New(BadRequest, "Sequence name cannot be longer than 50 characters.")
	ErrSequenceSubject = New(BadRequest, "Sequence subject cannot be longer than 100 characters.")
	ErrSequenceBody    = New(BadRequest, fmt.Sprintf("Sequence body cannot be longer than %d characters.", config.SequenceBodyLimit))

	// Contact
	ErrContactSerialize = New(BadRequest, "Failed to serialize contact.")
	ErrContactSize      = New(BadRequest, "Contact size cannot be bigger than 10KB.")

	// Unibox
	ErrUniboxLimit = New(BadRequest, fmt.Sprintf("Limit must be between %d and %d.", config.UniboxLimitMin, config.UniboxLimitMax))
	ErrSeenMax     = New(BadRequest, "Cannot update more than 500 messages.")

	// Servers
	ErrIPAddr    = New(BadRequest, "Invalid IP Address.")
	ErrPublicKey = New(BadRequest, "Invalid Public Key.")
)
