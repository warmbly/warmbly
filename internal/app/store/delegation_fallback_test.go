package store

import (
	"context"
	"net/http"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// refusesMicrosoft records each delegation push and refuses any that carries a Microsoft identity.
type refusesMicrosoft struct {
	graphkit.API
	pushed []graphkit.Delegation
}

func (r *refusesMicrosoft) PutDelegation(_ context.Context, d graphkit.Delegation, _ string) error {
	r.pushed = append(r.pushed, d)
	if d.Microsoft != nil {
		return &graphkit.Error{Status: http.StatusBadRequest, Code: graphkit.CodeInvalidRequest, Message: `"User.Read.All" is not supported`}
	}
	return nil
}

func TestRefusedMicrosoftIdentityStillRegistersGoogle(t *testing.T) {
	h := newHarness(t)
	h.delegator.googleID = "1234567890"
	api := &refusesMicrosoft{API: h.fake}
	h.s.setAPI(api)
	if err := h.s.PushDelegation(context.Background()); err != nil {
		t.Fatalf("Google alone should register: %v", err)
	}
	if len(api.pushed) != 2 || api.pushed[1].Google == nil || api.pushed[1].Microsoft != nil {
		t.Fatalf("pushes: %+v", api.pushed)
	}
}
