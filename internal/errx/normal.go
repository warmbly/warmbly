package errx

import "errors"

var (
	ErrInvalidEventFormat = errors.New("invalid event format")
	ErrResourceNotFound   = errors.New("resource not found")
)
