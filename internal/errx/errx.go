package errx

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
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
}

// Error implements error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s (%d): %s", codeToString[e.Code], e.Code, e.Message)
}

// New creates a new business error.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
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
	return codeToIdentifier[e.Code]
}

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
	RequestID string `json:"request_id,omitempty"`
}

func InternalError() *Error {
	return New(Internal, "Something went wrong.")
}

func Handle(c *gin.Context, err error) {
	var bizErr *Error
	if errors.As(err, &bizErr) {
		// Business error – send clean JSON
		httpCode := codeToHTTP[bizErr.Code]
		httpError := codeToString[bizErr.Code]
		c.JSON(httpCode, response{
			Error:     httpError,
			Message:   bizErr.Message,
			Code:      bizErr.identifier(),
			RequestID: c.GetString("request_id"),
		})
		return
	}

	// Unexpected error → treat as internal
	Handle(c, InternalError())
}

// JSON sends a business error as JSON response
func JSON(c *gin.Context, err *Error) {
	httpCode := codeToHTTP[err.Code]
	httpError := codeToString[err.Code]
	c.JSON(httpCode, response{
		Error:     httpError,
		Message:   err.Message,
		Code:      err.identifier(),
		RequestID: c.GetString("request_id"),
	})
}
