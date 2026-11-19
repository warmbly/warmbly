package notify

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

// skipUndeliverable drops recipients under the reserved .invalid top-level domain (RFC 2606),
// which is where accounts that never receive mail (a reseller's service account and the people
// it signs in) are addressed, so platform mail to them never becomes a bounce.
type skipUndeliverable struct{ EmailNotificationService }

func deliverable(addrs []string) []string {
	out := addrs[:0:0]
	for _, a := range addrs {
		if !Undeliverable(a) {
			out = append(out, a)
		}
	}
	return out
}

// Undeliverable reports whether an address is under the reserved .invalid domain.
func Undeliverable(addr string) bool {
	return models.InternalAddress(addr)
}

func (s skipUndeliverable) Send(ctx context.Context, to, cc, bcc []string, subject, message string) error {
	to, cc, bcc = deliverable(to), deliverable(cc), deliverable(bcc)
	if len(to)+len(cc)+len(bcc) == 0 {
		return nil
	}
	return s.EmailNotificationService.Send(ctx, to, cc, bcc, subject, message)
}

func (s skipUndeliverable) SendOutreach(ctx context.Context, to []string, replyTo, subject, message string) error {
	if to = deliverable(to); len(to) == 0 {
		return nil
	}
	return s.EmailNotificationService.SendOutreach(ctx, to, replyTo, subject, message)
}
