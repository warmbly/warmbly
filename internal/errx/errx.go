package errx

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// Error represents a business error with a code and message.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	// Identifier optionally overrides the machine-readable `code` in the JSON
	// response. Without it every error of the same HTTP class is indistinguishable
	// to a client, so a caller that needs to branch on a specific condition has
	// nothing stable to match on. Empty means "derive it from Code".
	Identifier string `json:"-"`
	// Public marks an Internal-class message that was written for the caller
	// and is safe to show. Internal messages are otherwise replaced with a
	// generic sentence, because they are usually built from the underlying
	// error. The exceptions are the few that tell an operator something they
	// can act on, like a mail transport that cannot deliver.
	Public bool `json:"-"`
	// Cause is a finer machine key than Identifier for callers that group
	// failures (a mailbox import), and Detail the scrubbed server reply behind
	// it. Neither is ever written to a response by Handle.
	Cause  string `json:"-"`
	Detail string `json:"-"`
	// Reason is an optional finer key written to the response, for a client
	// that offers a different next step within one Identifier.
	Reason string `json:"-"`
	// RetryAfter, in seconds, is sent as the Retry-After header when set.
	RetryAfter int `json:"-"`
}

// Error implements error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s (%d): %s", codeToString[e.Code], e.Code, e.Message)
}

// New creates a new business error.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NewPublic creates an error whose message is shown to the caller even when it
// is server-class. Use it only for a message a person can act on, never for one
// derived from an underlying error.
func NewPublic(code Code, message string) *Error {
	return &Error{Code: code, Message: message, Public: true}
}

// NewWithIdentifier creates a business error carrying its own machine-readable
// identifier, for conditions a client is expected to detect and handle
// specifically rather than just display.
func NewWithIdentifier(code Code, identifier, message string) *Error {
	return &Error{Code: code, Message: message, Identifier: identifier}
}

// identifier returns the response `code`: the error's own when set, otherwise
// the generic one for its HTTP class.
func (e *Error) identifier() string {
	if e.Identifier != "" {
		return e.Identifier
	}
	if id, ok := codeToIdentifier[e.Code]; ok {
		return id
	}
	return codeToIdentifier[Internal]
}

// resolve is the HTTP status and title to answer with. A Code outside the
// table maps to 0, which gin leaves at 200, so an error would be reported as a
// success; anything unknown is an internal error instead.
func (e *Error) resolve() (int, string) {
	if status, ok := codeToHTTP[e.Code]; ok {
		return status, codeToString[e.Code]
	}
	return codeToHTTP[Internal], codeToString[Internal]
}

// ResponseCode is the machine-readable `code` this error answers with, for
// callers that embed errors in a body of their own (per-row results).
func (e *Error) ResponseCode() string { return e.identifier() }

// --- Predefined errors (exported) ---
var (
	ErrUnauthorized  = New(Unauthorized, "Token not found.")
	ErrForbidden     = New(Forbidden, "You don't have access to this feature.")
	ErrNotFound      = New(NotFound, "Resource not found.")
	ErrConflict      = New(Conflict, "resource already exists")
	ErrUnprocessable = New(Unprocessable, "validation failed")
	ErrServiceDown   = New(ServiceUnavailable, "service unavailable")
)

// --- Gin handler helper ---
type response struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Code      string `json:"code"`
	Reason    string `json:"reason,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func InternalError() *Error {
	return New(Internal, "Something went wrong.")
}

func Handle(c *gin.Context, err error) {
	var bizErr *Error
	if errors.As(err, &bizErr) {
		JSON(c, bizErr)
		return
	}

	// Unexpected error → treat as internal
	Handle(c, InternalError())
}

// StatusClientClosedRequest answers a request whose caller hung up first.
const StatusClientClosedRequest = 499

// JSON sends a business error as JSON response.
func JSON(c *gin.Context, err *Error) {
	// A query cut short because the caller went away is the caller's doing, not a server error.
	if err.Code == Internal && c.Request != nil && errors.Is(c.Request.Context().Err(), context.Canceled) {
		c.AbortWithStatus(StatusClientClosedRequest)
		return
	}
	httpCode, httpError := err.resolve()
	if err.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(err.RetryAfter))
	}
	c.JSON(httpCode, response{
		Error:     httpError,
		Message:   clientMessage(c, err),
		Code:      err.identifier(),
		Reason:    err.Reason,
		RequestID: c.GetString("request_id"),
	})
}

// genericServerMessage is the only thing a 5xx says to a caller.
const genericServerMessage = "Something went wrong."

// clientMessage is what the caller is told.
//
// An Internal message describes something that went wrong inside the service,
// and handlers routinely built one from the underlying error: driver text with
// SQLSTATE codes and column names, provider responses, file paths. None of that
// helps the caller and all of it helps somebody mapping the system.
//
// So an Internal error answers with one fixed sentence and the request id. The
// real message is logged against that id, which is where an operator should be
// reading it from anyway.
//
// Only Internal. The other server-class codes carry messages a developer wrote
// for the caller and that the caller can act on ("no mailbox workers are
// available right now", "this provider is not configured on this instance"),
// and blanking those would replace working guidance with a shrug.
func clientMessage(c *gin.Context, err *Error) string {
	status, _ := err.resolve()
	if err.Code != Internal || err.Public {
		return err.Message
	}
	if detail := err.Message; detail != "" && detail != genericServerMessage {
		log.Error().
			Str("request_id", c.GetString("request_id")).
			Str("path", c.FullPath()).
			Int("status", status).
			Str("detail", detail).
			Msg("server error returned to client")
	}
	return genericServerMessage
}
