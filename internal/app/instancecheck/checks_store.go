package instancecheck

import (
	"context"
	"fmt"
)

const docsStore = "/development/configuration/#purchases-with-no-key"

func storeChecks() []check {
	return []check{{id: "store_purchases_without_graphkit", run: checkStorePurchases}}
}

// checkStorePurchases fires when GraphKit is not configured but domains or mailboxes bought here are still live.
func checkStorePurchases(ctx context.Context, d Deps, _ Input) *Finding {
	if d.Store == nil {
		return nil
	}
	mailboxes, domains, err := d.Store.UnreachablePurchases(ctx)
	if err != nil || mailboxes+domains == 0 {
		return nil
	}
	return result(CategoryData, SeverityError, "Purchases are live but GraphKit is not configured",
		fmt.Sprintf("%s and %s bought here are still live. Renewals, monthly billing and Google mailbox connections are paused until GraphKit is configured. "+
			"Add the GraphKit API key under Setup and health > Integrations, or set GRAPHKIT_API_KEY.", plural(mailboxes, "mailbox", "mailboxes"), plural(domains, "domain", "domains")),
		docsStore)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
