package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/twofa"
	"github.com/warmbly/warmbly/internal/errx"
)

type reauthFactorStub struct {
	twofa.Service
	valid bool
	xerr  *errx.Error
}

func (s reauthFactorStub) VerifyCurrentCode(context.Context, uuid.UUID, string) (bool, *errx.Error) {
	return s.valid, s.xerr
}

func TestReauthPreservesAuthenticatorVerificationOutcome(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
		xerr  *errx.Error
	}{
		{name: "accepted", valid: true},
		{name: "mismatch"},
		{name: "unavailable", xerr: errx.InternalError()},
		{name: "used", xerr: errx.NewWithIdentifier(errx.BadRequest, twofa.UsedCodeID, "Wait for the next code.")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{TwoFAService: reauthFactorStub{valid: tc.valid, xerr: tc.xerr}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/auth/reauth", nil)
			valid, xerr := h.reauthProofValid(c, uuid.New(), reauthRequest{Code: "123456"})
			if valid != tc.valid || xerr != tc.xerr {
				t.Fatalf("accepted=%t error=%v; want accepted=%t error=%v", valid, xerr, tc.valid, tc.xerr)
			}
		})
	}
}
