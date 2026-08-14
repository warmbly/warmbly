package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBusinessDayWindowTable(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"sat_10", time.Date(2026, 8, 8, 10, 0, 0, 0, loc), false},
		{"sun_10", time.Date(2026, 8, 9, 10, 0, 0, 0, loc), false},
		{"mon_0859", time.Date(2026, 8, 10, 8, 59, 0, 0, loc), false},
		{"mon_0900", time.Date(2026, 8, 10, 9, 0, 0, 0, loc), true},
		{"fri_1759", time.Date(2026, 8, 14, 17, 59, 0, 0, loc), true},
		{"fri_1800", time.Date(2026, 8, 14, 18, 0, 0, 0, loc), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InSendWindowBusiness(tc.t.UTC(), "America/Sao_Paulo", "09:00", "18:00", true)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("want %v got %v local=%s wd=%s", tc.want, got, tc.t, tc.t.Weekday())
			}
		})
	}
}

func TestGovernorDefersWeekend(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	clock := &FixedClock{T: time.Date(2026, 8, 8, 10, 0, 0, 0, loc).UTC()}
	cfg := DefaultConfig()
	cfg.BusinessDaysOnly = true
	cfg.MinGap = 0
	g := NewGovernor(cfg, NewMemoryStore(), clock)
	res, err := g.TryReserve(context.Background(), ReserveRequest{
		OrganizationID: uuid.New(),
		Channel:        ChannelEmail,
		MessageKey:     "email:draft:weekend",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("weekend must not allow reserve")
	}
	if res.Reason != "outside_business_day" {
		t.Fatalf("reason=%s", res.Reason)
	}
}
