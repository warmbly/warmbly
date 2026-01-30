package goog

import (
	"fmt"
	"strings"
)

func (c *Client) GetAddress() string {
	return fmt.Sprintf("%s <%s>", strings.TrimSpace(c.FirstName+" "+c.LastName), c.Email)
}
