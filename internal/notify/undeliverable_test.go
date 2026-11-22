package notify

import (
	"context"
	"testing"
)

type recordSend struct{ to [][]string }

func (r *recordSend) Send(_ context.Context, to, _, _ []string, _, _ string) error {
	r.to = append(r.to, to)
	return nil
}

func (r *recordSend) SendOutreach(_ context.Context, to []string, _, _, _ string) error {
	r.to = append(r.to, to)
	return nil
}

// Accounts addressed under .invalid (a reseller's service account and the people it signs in) never get platform mail.
func TestPlatformMailSkipsReservedInvalidAddresses(t *testing.T) {
	rec := &recordSend{}
	s := skipUndeliverable{rec}
	_ = s.Send(context.Background(), []string{"p-1@acme.partner.invalid"}, nil, nil, "s", "m")
	if len(rec.to) != 0 {
		t.Fatalf("mail to an .invalid address was sent: %v", rec.to)
	}
	_ = s.Send(context.Background(), []string{"Ada <ada@example.com>", "p-1@acme.partner.INVALID"}, nil, nil, "s", "m")
	if len(rec.to) != 1 || len(rec.to[0]) != 1 || rec.to[0][0] != "Ada <ada@example.com>" {
		t.Fatalf("recipients = %v", rec.to)
	}
}
