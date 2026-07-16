package errx

import "net/http"

type Code int

const (
	BadRequest         Code = http.StatusBadRequest
	Unauthorized       Code = http.StatusUnauthorized
	PaymentRequired    Code = http.StatusPaymentRequired
	Forbidden          Code = http.StatusForbidden
	NotFound           Code = http.StatusNotFound
	Conflict           Code = http.StatusConflict
	Unprocessable      Code = http.StatusUnprocessableEntity
	TooManyRequests    Code = http.StatusTooManyRequests
	Internal           Code = http.StatusInternalServerError
	NotImplemented     Code = http.StatusNotImplemented
	ServiceUnavailable Code = http.StatusServiceUnavailable
)

var codeToHTTP = map[Code]int{
	BadRequest:         http.StatusBadRequest,
	Unauthorized:       http.StatusUnauthorized,
	PaymentRequired:    http.StatusPaymentRequired,
	Forbidden:          http.StatusForbidden,
	NotFound:           http.StatusNotFound,
	Conflict:           http.StatusConflict,
	Unprocessable:      http.StatusUnprocessableEntity,
	TooManyRequests:    http.StatusTooManyRequests,
	Internal:           http.StatusInternalServerError,
	NotImplemented:     http.StatusNotImplemented,
	ServiceUnavailable: http.StatusServiceUnavailable,
}

var codeToString = map[Code]string{
	BadRequest:         "Bad Request",
	Unauthorized:       "Unauthorized",
	PaymentRequired:    "Payment Required",
	Forbidden:          "Forbidden",
	NotFound:           "Not Found",
	Conflict:           "Conflict",
	Unprocessable:      "Unprocessable",
	TooManyRequests:    "Too Many Requests",
	Internal:           "Internal Server Error",
	NotImplemented:     "Not Implemented",
	ServiceUnavailable: "Service Unavailable",
}

var codeToIdentifier = map[Code]string{
	BadRequest:   "bad_request",
	Unauthorized: "unauthorized",
	// 402 is only ever raised for a depleted AI-credit balance in this app.
	PaymentRequired:    "insufficient_credits",
	Forbidden:          "forbidden",
	NotFound:           "not_found",
	Conflict:           "conflict",
	Unprocessable:      "unprocessable",
	TooManyRequests:    "rate_limit_exceeded",
	Internal:           "internal_error",
	NotImplemented:     "not_implemented",
	ServiceUnavailable: "service_unavailable",
}
