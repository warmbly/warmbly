// Package textcontent classifies supported UTF-8 text without changing its bytes.
package textcontent

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

var ErrNotText = errors.New("file must contain UTF-8 text without binary control characters")

// Validate permits a UTF-8 BOM, Unicode, and ordinary text whitespace.
func Validate(data []byte) error {
	if !utf8.Valid(data) {
		return ErrNotText
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' && r != '\f' {
			return ErrNotText
		}
	}
	return nil
}
