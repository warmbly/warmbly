package instancecheck

import (
	"context"
	"strings"
	"testing"
)

type purchases struct{ mailboxes, domains int }

func (p purchases) UnreachablePurchases(context.Context) (int, int, error) {
	return p.mailboxes, p.domains, nil
}

func TestPurchasesWithoutGraphKitIsAnError(t *testing.T) {
	if f := checkStorePurchases(context.Background(), Deps{Store: purchases{}}, Input{}); f != nil {
		t.Fatalf("nothing live: %+v", f)
	}
	if f := checkStorePurchases(context.Background(), Deps{}, Input{}); f != nil {
		t.Fatalf("no store: %+v", f)
	}
	f := checkStorePurchases(context.Background(), Deps{Store: purchases{mailboxes: 3, domains: 1}}, Input{})
	if f == nil || f.Severity != SeverityError || !strings.Contains(f.Message, "3 mailboxes and 1 domain") || !strings.Contains(f.Message, "paused until GraphKit is configured") {
		t.Fatalf("live purchases: %+v", f)
	}
}
