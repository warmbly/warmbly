package email

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestRequireAddress(t *testing.T) {
	if xerr := requireAddress(models.InboxProviderOutlook, "Jordan@Client.test ", "jordan@client.test"); xerr != nil {
		t.Fatalf("same mailbox, other case = %v", xerr)
	}
	if xerr := requireAddress(models.InboxProviderOutlook, "anyone@else.test", ""); xerr != nil {
		t.Fatalf("no required address = %v", xerr)
	}
	xerr := requireAddress(models.InboxProviderOutlook, "admin@client.onmicrosoft.com", "jordan@client.test")
	if xerr == nil || xerr.ResponseCode() != ErrSignedInAsOther {
		t.Fatalf("another account = %v", xerr)
	}
	for _, want := range []string{"Microsoft", "admin@client.onmicrosoft.com", "jordan@client.test"} {
		if !strings.Contains(xerr.Message, want) {
			t.Errorf("message %q lacks %q", xerr.Message, want)
		}
	}
}
