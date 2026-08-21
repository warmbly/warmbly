package msgraph

import (
	"net/mail"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
)

// GetAddress renders the mailbox's RFC 5322 From value ("First Last <email>"),
// RFC 2047-encoding a non-ASCII display name so it does not reach the
// recipient as mojibake.
func (c *Client) GetAddress() string {
	addr := mail.Address{Name: strings.TrimSpace(c.FirstName + " " + c.LastName), Address: c.Email}
	return mailhdr.Address(addr.String())
}
