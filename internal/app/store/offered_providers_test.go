package store

import (
	"slices"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

func TestOfferedProvidersFollowThePriceList(t *testing.T) {
	google := &graphkit.Pricing{Mailboxes: []graphkit.MailboxPrice{{Provider: graphkit.ProviderGoogle, MonthlyMicros: 2_000_000}}}
	if got := offeredProviders(google); !slices.Equal(got, []string{graphkit.ProviderGoogle}) {
		t.Fatalf("google only: %v", got)
	}
	both := &graphkit.Pricing{Mailboxes: []graphkit.MailboxPrice{{Provider: graphkit.ProviderMicrosoft}, {Provider: graphkit.ProviderGoogle}}}
	if got := offeredProviders(both); !slices.Equal(got, []string{graphkit.ProviderGoogle, graphkit.ProviderMicrosoft}) {
		t.Fatalf("both: %v", got)
	}
	if err := providerUnavailable(graphkit.ProviderMicrosoft); err == nil || err.Identifier != CodeProviderUnavailable {
		t.Fatalf("refusal: %+v", err)
	}
}
