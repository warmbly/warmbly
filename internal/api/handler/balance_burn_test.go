package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type burnCredits struct {
	credits.CreditService
	burnReads int
}

func (c *burnCredits) Unmetered() bool { return false }
func (c *burnCredits) GetLedger(context.Context, uuid.UUID) (*models.CreditLedger, *errx.Error) {
	return &models.CreditLedger{}, nil
}
func (c *burnCredits) AgencyAvailable(context.Context, uuid.UUID) models.Micro { return 0 }
func (c *burnCredits) AIDailyAverage(context.Context, uuid.UUID, int) (models.Micro, *errx.Error) {
	c.burnReads++
	return 0, nil
}

// The dashboard reads the balance on every page; only the billing page asks for the daily draw, which sums a month of the ledger.
func TestBalanceReadComputesTheDailyDrawOnlyWhenAsked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cs := &burnCredits{}
	h := &Handler{CreditService: cs}
	read := func(target string) map[string]any {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		c.Set(middleware.OrganizationIDKey, uuid.New())
		h.GetBalance(c)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", target, w.Code, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}

	if out := read("/subscription/balance"); out["burn"] != nil || cs.burnReads != 0 {
		t.Fatalf("plain read: burn %v, %d ledger sums", out["burn"], cs.burnReads)
	}
	if out := read("/subscription/balance?include=burn"); out["burn"] == nil || cs.burnReads != 1 {
		t.Fatalf("include=burn: burn %v, %d ledger sums", out["burn"], cs.burnReads)
	}
}
