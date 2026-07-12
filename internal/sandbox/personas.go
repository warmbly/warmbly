package sandbox

import (
	"fmt"
	"hash/fnv"
)

// persona is a contact's deterministic behavior profile, derived from the
// email address so it is stable across simulator restarts with no DB state.
type persona struct {
	Opens   bool
	Clicks  bool
	Replies bool
	Flavor  replyFlavor
}

type replyFlavor int

const (
	replyPositive replyFlavor = iota
	replyQuestion
	replyNegative
	replyOutOfOffice
)

// personaFor buckets a contact by hash: most open, some click, some reply,
// and reply tone varies so the reply classifier has real work to do.
func personaFor(email string) persona {
	h := fnv.New32a()
	h.Write([]byte(email))
	n := h.Sum32()

	p := persona{
		Opens:   n%100 < 85,
		Clicks:  n%7 < 3,  // ~43% of openers
		Replies: n%11 < 4, // ~36% of openers
	}
	switch n % 10 {
	case 0, 1, 2, 3:
		p.Flavor = replyPositive
	case 4, 5:
		p.Flavor = replyQuestion
	case 6, 7:
		p.Flavor = replyNegative
	default:
		p.Flavor = replyOutOfOffice
	}
	return p
}

// replyBody returns the reply text plus whether the reply is an automated one
// (out-of-office), which gets auto-reply headers so the classifier can gate it
// out of human-reply stats.
func replyBody(f replyFlavor, firstName string) (body string, automated bool) {
	switch f {
	case replyPositive:
		return fmt.Sprintf("Hi,\n\nThis looks interesting. Can you send over pricing and a couple of customer references?\n\nThanks,\n%s", firstName), false
	case replyQuestion:
		return fmt.Sprintf("Hi,\n\nBefore we go further: does this integrate with our existing CRM, and where is the data hosted?\n\n%s", firstName), false
	case replyNegative:
		return fmt.Sprintf("Hi,\n\nNot a fit for us right now. Please remove me from this list.\n\n%s", firstName), false
	default:
		return "Hello,\n\nI am currently out of the office with limited access to email and will respond on my return.\n\nThis is an automated response.", true
	}
}

// userAgents rotate across opens/clicks so tracking data looks organic. None
// of these match the tracking service's prefetch/scanner filter list.
var userAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
}
