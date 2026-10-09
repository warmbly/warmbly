package goog

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/stoken"
	"golang.org/x/oauth2"
)

func TestProviderDefaultRequestDeadline(t *testing.T) {
	for _, brokered := range []bool{false, true} {
		c := &Client{}
		token := &oauth2.Token{AccessToken: "local-test"}
		var err error
		if brokered {
			if merr := c.InitWithSource(context.Background(), oauth2.StaticTokenSource(token)); merr != nil {
				err = merr
			}
		} else {
			if merr := c.Init(context.Background(), token, oauth2.Config{}); merr != nil {
				err = merr
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.rawClient.Timeout != stoken.ProviderRequestTimeout {
			t.Fatalf("brokered=%v default timeout=%v", brokered, c.rawClient.Timeout)
		}
	}
}
