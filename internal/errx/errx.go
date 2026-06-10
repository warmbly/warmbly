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
}

// Error implements error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s (%d): %s", codeToString[e.Code], e.Code, e.Message)
}

// New creates a new business error.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
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
			Code:      codeToIdentifier[bizErr.Code],
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
		Code:      codeToIdentifier[err.Code],
		RequestID: c.GetString("request_id"),
	})
}
