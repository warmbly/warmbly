// Package listquality measures a batch of addresses at import time.
//
// This is deliberately NOT verification. Verification asks a mail server
// whether an address exists and runs asynchronously; this reads the addresses
// themselves, synchronously, so a customer learns something about a list the
// moment they upload it rather than hours later.
package listquality

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/signuprisk"
)

// rolePrefixes are shared-inbox local parts. Same vocabulary the advisor and
// the launch gate use, so one list is never described three different ways.
var rolePrefixes = map[string]bool{
	"info": true, "sales": true, "support": true, "contact": true,
	"admin": true, "hello": true, "help": true, "office": true, "team": true,
	"billing": true, "careers": true, "jobs": true, "marketing": true,
	"noreply": true, "no-reply": true, "webmaster": true,
	"enquiries": true, "enquiry": true,
}

// Summary is what one import's addresses look like.
type Summary struct {
	Total int
	// Malformed are addresses that are not addresses at all.
	Malformed int
	// Disposable are throwaway-domain addresses.
	Disposable int
	// Role are shared inboxes, which reply rarely and complain more.
	Role int
	// BadSharePct is malformed plus disposable, as a percentage. Role
	// addresses are deliberately excluded: mailing info@ is a choice, not a
	// defect, and plenty of legitimate B2B lists are full of them.
	BadSharePct float64
	// Flagged is true when the list is bad enough to tell the customer about.
	Flagged bool
	// Summary is the sentence they read.
	Summary string
}

const (
	// FlagSharePct is where a list is called out. Below this a few bad
	// addresses in a big list are just normal data entry.
	FlagSharePct = 25.0
	// MinSample is the size below which a share means nothing.
	MinSample = 20
)

// Assess scores a batch. It never rejects an import: the addresses are the
// customer's own data, and refusing to store them is a different and much
// larger decision than refusing to send to them. The launch gate is where a
// bad list is actually stopped.
func Assess(emails []string) Summary {
	s := Summary{Total: len(emails)}
	for _, raw := range emails {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		if _, err := mail.ParseAddress(addr); err != nil {
			s.Malformed++
			continue
		}
		if signuprisk.IsDisposable(addr) {
			s.Disposable++
			continue
		}
		if at := strings.Index(addr, "@"); at > 0 && rolePrefixes[strings.ToLower(addr[:at])] {
			s.Role++
		}
	}

	if s.Total == 0 {
		return s
	}
	s.BadSharePct = float64(s.Malformed+s.Disposable) / float64(s.Total) * 100

	if s.Total >= MinSample && s.BadSharePct >= FlagSharePct {
		s.Flagged = true
		s.Summary = fmt.Sprintf(
			"%.0f%% of this list is unusable: %d malformed and %d throwaway addresses out of %d.",
			s.BadSharePct, s.Malformed, s.Disposable, s.Total)
	}
	return s
}
