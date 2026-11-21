package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Stable error codes of /v1/linkedin, documented in api/error-codes.mdx.
const (
	CodeUnavailable             = "linkedin_unavailable"
	CodeModeConflict            = "linkedin_mode_conflict"
	CodePlanRequired            = "linkedin_plan_required"
	CodeCheckoutRequired        = "linkedin_checkout_required"
	CodeAcceptRequired          = "linkedin_accept_required"
	CodeAcknowledgementRequired = "linkedin_acknowledgement_required"
	CodeSignInFailed            = "linkedin_sign_in_failed"
	CodeTooManySignIns          = "linkedin_too_many_sign_ins"
	CodeSessionExpired          = "linkedin_session_expired"
	CodeUnpaid                  = "linkedin_unpaid"
	CodeInvalidRequest          = "linkedin_invalid_request"
	CodeLicenceRequired         = "licence_required"
	CodeSearchLimitReached      = "search_limit_reached"
	CodeSearchBusy              = "search_busy"
	CodeStaleCursor             = "stale_cursor"
	CodeRecipientInvalid        = "recipient_invalid"
	CodeAccountUnavailable      = "account_unavailable"
	CodeAIUnavailable           = "ai_unavailable"
)

// CheckoutRequiredError is a linked GraphKit plan that cannot add a seat; URL is GraphKit's billing page.
type CheckoutRequiredError struct{ URL string }

func (e *CheckoutRequiredError) Error() string { return CodeCheckoutRequired }

// AcceptRequiredError asks the caller to accept a charge first; Quote is what it would be.
type AcceptRequiredError struct{ Quote json.RawMessage }

func (e *AcceptRequiredError) Error() string { return CodeAcceptRequired }

// FieldsError is a settings change GraphKit refused per field.
type FieldsError struct {
	Message string
	Fields  json.RawMessage
}

func (e *FieldsError) Error() string { return CodeInvalidRequest }

func newErr(code errx.Code, ident, msg string) *errx.Error {
	e := errx.NewWithIdentifier(code, ident, msg)
	e.Public = true
	return e
}

var (
	errUnavailable     = newErr(errx.ServiceUnavailable, CodeUnavailable, "LinkedIn is not available on this instance.")
	errPlanRequired    = newErr(errx.PaymentRequired, CodePlanRequired, "Connecting a LinkedIn account needs a Warmbly plan. Choose a plan, or link your own GraphKit workspace.")
	errPartnerManaged  = newErr(errx.Forbidden, store.CodeManagedByPartner, "This workspace's LinkedIn seats are bought through its partner.")
	errAcknowledgement = newErr(errx.BadRequest, CodeAcknowledgementRequired, "Accept the terms for automating a LinkedIn account to continue.")
	errNotFound        = errx.NewWithIdentifier(errx.NotFound, "not_found", "Not found.")
	errSessionUnpaid   = newErr(errx.Conflict, CodeSessionExpired, "This connection was not paid for. Start a new one.")
	errUnpaid          = newErr(errx.PaymentRequired, CodeUnpaid, "This account is paused because its month went unpaid. Update your card and it resumes with the next payment.")
	errRemoved         = newErr(errx.Conflict, CodeAccountUnavailable, "This account was removed.")
	errBadQuery        = newErr(errx.BadRequest, CodeInvalidRequest, "query must be a JSON object.")
)

func invalid(msg string) *errx.Error { return newErr(errx.BadRequest, CodeInvalidRequest, msg) }

// upstream maps a GraphKit refusal to a LinkedIn code, falling back to the store's mapping.
func (s *Service) upstream(ctx context.Context, op string, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	ge, ok := graphkit.AsError(err)
	if !ok {
		return s.Store.Upstream(ctx, op, err)
	}
	withRetry := func(e *errx.Error) *errx.Error {
		if ge.RetryAfter > 0 {
			e.RetryAfter = int(max(ge.RetryAfter, time.Second) / time.Second)
		} else if e.Code == errx.ServiceUnavailable || e.Code == errx.TooManyRequests {
			e.RetryAfter = 30
		}
		return e
	}
	switch ge.Code {
	case graphkit.CodeSocialUnavailable:
		return withRetry(newErr(errx.ServiceUnavailable, CodeUnavailable, "LinkedIn is unavailable right now. Try again in a few minutes."))
	case graphkit.CodeSocialModeConflict:
		return newErr(errx.Conflict, CodeModeConflict, "Remove this workspace's LinkedIn accounts before linking a GraphKit workspace.")
	case graphkit.CodeSocialCheckoutRequired:
		return &CheckoutRequiredError{URL: ge.CheckoutURL()}
	case graphkit.CodeStateConflict:
		if q := ge.Quote(); len(q) > 0 {
			return &AcceptRequiredError{Quote: q}
		}
		return newErr(errx.Conflict, "linkedin_state_conflict", firstNonEmpty(ge.Message, "This changed meanwhile. Reload and try again."))
	case graphkit.CodeSignInFailed:
		return newErr(errx.Unprocessable, CodeSignInFailed, firstNonEmpty(ge.Message, "LinkedIn did not accept these details."))
	case graphkit.CodeTooManyFailedSignIns:
		return withRetry(newErr(errx.TooManyRequests, CodeTooManySignIns, "Too many sign-ins were refused for this account today. Try again tomorrow."))
	case graphkit.CodeSessionExpired:
		return newErr(errx.Conflict, CodeSessionExpired, "This connection expired. Start a new one.")
	case graphkit.CodeLicenceRequired:
		return newErr(errx.Conflict, CodeLicenceRequired, firstNonEmpty(ge.Message, "This search needs a LinkedIn account with the right licence."))
	case graphkit.CodeSearchLimitReached:
		return newErr(errx.Conflict, CodeSearchLimitReached, firstNonEmpty(ge.Message, "This account reached its search limit for today."))
	case graphkit.CodeSearchBusy:
		return withRetry(newErr(errx.ServiceUnavailable, CodeSearchBusy, "LinkedIn search is busy. Try again shortly."))
	case graphkit.CodeStaleCursor:
		return newErr(errx.Conflict, CodeStaleCursor, "The search changed. Run it again from the first page.")
	case graphkit.CodeRecipientInvalid:
		return newErr(errx.Unprocessable, CodeRecipientInvalid, firstNonEmpty(ge.Message, "That LinkedIn profile could not be found."))
	case graphkit.CodeAccountUnavailable:
		return newErr(errx.Conflict, CodeAccountUnavailable, firstNonEmpty(ge.Message, "This LinkedIn account can't be used right now."))
	case graphkit.CodeAIUnavailable:
		return withRetry(newErr(errx.ServiceUnavailable, CodeAIUnavailable, "The search builder is unavailable right now. Try again shortly."))
	case graphkit.CodeNotFound:
		return errNotFound
	case graphkit.CodeInvalidRequest:
		var d struct {
			Fields json.RawMessage `json:"fields"`
		}
		if len(ge.Details) > 0 {
			_ = json.Unmarshal(ge.Details, &d)
		}
		if len(d.Fields) > 0 {
			return &FieldsError{Message: firstNonEmpty(ge.Message, "Some settings are out of range."), Fields: d.Fields}
		}
		return invalid(firstNonEmpty(ge.Message, "The request was not valid."))
	}
	return s.Store.Upstream(ctx, op, err)
}

// paymentError turns a balance refusal into the store's payment codes.
func paymentError(err error) error {
	if errors.Is(err, credits.ErrInsufficientCredits) {
		return newErr(errx.PaymentRequired, store.CodeInsufficientBalance, "Your balance can't cover this. Add money and try again.")
	}
	return err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
