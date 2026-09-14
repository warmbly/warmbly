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
	return c.FromAddress("")
}

// FromAddress is GetAddress with a per-send display name; empty falls back to
// the name the client was built with.
func (c *Client) FromAddress(name string) string {
	return c.FromIdentity(name, "")
}

// FromIdentity is the From header for a send that may go out under one of the
// mailbox's verified send-as aliases. An empty address is the mailbox's own,
// which is every send until someone picks an alias; Gmail refuses an address
// the account has not verified, so the choice is checked before it is stored
// rather than here.
func (c *Client) FromIdentity(name, email string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(c.FirstName + " " + c.LastName)
	}
	addr := mail.Address{Name: name, Address: c.Email}
	if e := strings.TrimSpace(email); e != "" {
		addr.Address = e
	}
	return addr.String()
}
