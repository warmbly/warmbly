package goog

import (
	"net/mail"
	"strings"
)

// GetAddress is the From header. mail.Address does the RFC 5322 quoting and
// RFC 2047 encoding a display name needs: sprintf-ing it together emits a bare
// 8-bit name for "Renée", and silently splits the address into two recipients
// for "Doe, Jane". Both matter now that every send builds its own headers.
func (c *Client) GetAddress() string {
	name := strings.TrimSpace(c.FirstName + " " + c.LastName)
	addr := mail.Address{Name: name, Address: c.Email}
	return addr.String()
}
