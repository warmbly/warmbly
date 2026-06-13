package oauth

import "net/http"

// OAuthError is a standard RFC 6749 / OAuth 2.1 error: a stable machine code plus
// a human description, rendered as {"error":...,"error_description":...}.
type OAuthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
	HTTPStatus  int    `json:"-"`
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

func newOAuthError(status int, code, desc string) *OAuthError {
	return &OAuthError{Code: code, Description: desc, HTTPStatus: status}
}

// The error constructors below cover every code the authorize + token endpoints
// can emit. Status codes follow RFC 6749 §5.2 (invalid_client is 401, the rest
// of the request errors are 400).
func errInvalidRequest(desc string) *OAuthError {
	return newOAuthError(http.StatusBadRequest, "invalid_request", desc)
}
func errInvalidClient(desc string) *OAuthError {
	return newOAuthError(http.StatusUnauthorized, "invalid_client", desc)
}
func errInvalidGrant(desc string) *OAuthError {
	return newOAuthError(http.StatusBadRequest, "invalid_grant", desc)
}
func errUnauthorizedClient(desc string) *OAuthError {
	return newOAuthError(http.StatusBadRequest, "unauthorized_client", desc)
}
func errUnsupportedGrantType(desc string) *OAuthError {
	return newOAuthError(http.StatusBadRequest, "unsupported_grant_type", desc)
}
func errInvalidScope(desc string) *OAuthError {
	return newOAuthError(http.StatusBadRequest, "invalid_scope", desc)
}
func errAccessDenied(desc string) *OAuthError {
	return newOAuthError(http.StatusForbidden, "access_denied", desc)
}
func errServer(desc string) *OAuthError {
	return newOAuthError(http.StatusInternalServerError, "server_error", desc)
}
