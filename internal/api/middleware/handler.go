package middleware

import (
	"github.com/warmbly/warmbly/internal/app/token"
)

type Handler struct {
	TokenService token.TokenService
}
