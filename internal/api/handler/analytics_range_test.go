package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// GET /analytics/campaigns/:id takes an optional period: both days or
// neither, each YYYY-MM-DD, from not after to (issue #702).
func TestOptionalDayRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query    string
		wantNil  bool
		wantErr  bool
		from, to string
	}{
		{query: "", wantNil: true},
		{query: "from=2026-09-01&to=2026-09-27", from: "2026-09-01", to: "2026-09-27"},
		{query: "from=2026-09-27&to=2026-09-27", from: "2026-09-27", to: "2026-09-27"},
		{query: "from=2026-09-01", wantErr: true},
		{query: "to=2026-09-27", wantErr: true},
		{query: "from=2026-09-28&to=2026-09-27", wantErr: true},
		{query: "from=09/01/2026&to=2026-09-27", wantErr: true},
		{query: "from=2026-09-01&to=2026-09-27T10:00:00Z", wantErr: true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/analytics/campaigns/x?"+tc.query, nil)
			period, xerr := optionalDayRange(c)
			if tc.wantErr {
				if xerr == nil {
					t.Fatalf("period = %+v, want a 400", period)
				}
				return
			}
			if xerr != nil {
				t.Fatalf("unexpected error: %v", xerr)
			}
			if tc.wantNil {
				if period != nil {
					t.Fatalf("period = %+v, want nil (all time)", period)
				}
				return
			}
			if got := period.From.Format(time.DateOnly); got != tc.from {
				t.Errorf("from = %s, want %s", got, tc.from)
			}
			if got := period.To.Format(time.DateOnly); got != tc.to {
				t.Errorf("to = %s, want %s", got, tc.to)
			}
		})
	}
}

// GET /analytics/dashboard takes campaign_ids and folder_ids as comma lists
// or repeated keys, deduplicated, refusing a malformed id (issue #869).
func TestUUIDListQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a, b := "0b8f6a52-6f0e-4c3e-9a51-0d3f1b0f6a01", "0b8f6a52-6f0e-4c3e-9a51-0d3f1b0f6a02"
	for _, tc := range []struct {
		query   string
		want    int
		wantErr bool
	}{
		{query: "", want: 0},
		{query: "campaign_ids=" + a, want: 1},
		{query: "campaign_ids=" + a + "," + b, want: 2},
		{query: "campaign_ids=" + a + "&campaign_ids=" + b, want: 2},
		{query: "campaign_ids=" + a + ",%20" + a + ",", want: 1},
		{query: "campaign_ids=" + a + ",nope", wantErr: true},
		{query: "campaign_ids=" + a + "," + b + "," + a, want: 2},
	} {
		t.Run(tc.query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/analytics/dashboard?"+tc.query, nil)
			ids, xerr := uuidListQuery(c, "campaign_ids", 2)
			if tc.wantErr {
				if xerr == nil {
					t.Fatalf("ids = %v, want a 400", ids)
				}
				return
			}
			if xerr != nil || len(ids) != tc.want {
				t.Fatalf("ids = %v, err = %v; want %d ids", ids, xerr, tc.want)
			}
		})
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/analytics/dashboard?campaign_ids="+a+","+b, nil)
	if ids, xerr := uuidListQuery(c, "campaign_ids", 1); xerr == nil {
		t.Fatalf("ids = %v past the limit, want a 400", ids)
	}
}
