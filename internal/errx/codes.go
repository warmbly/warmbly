package errx

import "net/http"

type Code int

const (
	BadRequest         Code = http.StatusBadRequest
	Unauthorized       Code = http.StatusUnauthorized
	Forbidden          Code = http.StatusForbidden
	NotFound           Code = http.StatusNotFound
	Conflict           Code = http.StatusConflict
	Unprocessable      Code = http.StatusUnprocessableEntity
	Internal           Code = http.StatusInternalServerError
	ServiceUnavailable Code = http.StatusServiceUnavailable
)

var codeToHTTP = map[Code]int{
	BadRequest:         http.StatusBadRequest,
	Unauthorized:       http.StatusUnauthorized,
	Forbidden:          http.StatusForbidden,
	NotFound:           http.StatusNotFound,
	Conflict:           http.StatusConflict,
	Unprocessable:      http.StatusUnprocessableEntity,
	Internal:           http.StatusInternalServerError,
	ServiceUnavailable: http.StatusServiceUnavailable,
}

var codeToString = map[Code]string{
	BadRequest:         "Bad Request",
	Unauthorized:       "Unauthorized",
	Forbidden:          "Forbidden",
	NotFound:           "Not Found",
	Conflict:           "Conflict",
	Unprocessable:      "Unprocessable",
	Internal:           "Internal Server Error",
	ServiceUnavailable: "Service Unavailable",
}
