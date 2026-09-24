package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestPlacementBuilderRollingReadsTheLookback(t *testing.T) {
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	sender := uuid.New()
	b := newPlacementBuilder(from, to)
	// Before the range: only the rolling rate may see it.
	b.addDelivery(repository.WarmupPlacementDayRow{SenderID: sender, Date: "2026-09-05", Group: "google", Inbox: 18, Spam: 2})
	b.addDelivery(repository.WarmupPlacementDayRow{SenderID: sender, Date: "2026-09-10", Group: "microsoft", Inbox: 4, Tabs: 1, Spam: 5, Rescued: 4})

	days := b.days()
	if len(days) != 2 {
		t.Fatalf("got %d days", len(days))
	}
	d := days[0]
	if d.Delivered != 10 || d.Spam != 5 || d.Rescued != 4 || *d.InboxRate != 50 {
		t.Fatalf("day counts: %+v", d.WarmupPlacementCounts)
	}
	if d.RollingInboxRate == nil || *d.RollingInboxRate != 76.67 {
		t.Fatalf("rolling rate over 30 deliveries: %v", d.RollingInboxRate)
	}
	if len(d.Groups) != 1 || d.Groups[0].Group != "microsoft" {
		t.Fatalf("groups: %+v", d.Groups)
	}
	if days[1].Delivered != 0 || days[1].InboxRate != nil {
		t.Fatalf("an empty day has no rate: %+v", days[1])
	}

	boxes := b.mailboxes(map[uuid.UUID]string{sender: "a@example.com"}, nil)
	if len(boxes) != 1 || boxes[0].Delivered != 10 || boxes[0].Rate.Band != models.WarmupPlacementBandNone {
		t.Fatalf("mailboxes: %+v", boxes)
	}
}

func TestApplyWarmupPlacementCapsHealth(t *testing.T) {
	h := models.AccountHealth{Status: "healthy", Score: 100}
	applyWarmupPlacement(&h, nil)
	if h.Score != 100 {
		t.Fatalf("no reading leaves health alone: %+v", h)
	}
	r := models.NewWarmupPlacementRate(97, 0, 3)
	applyWarmupPlacement(&h, &r)
	if h.Score != 97 || h.Status != "healthy" || len(h.Issues) != 0 {
		t.Fatalf("97%% caps the score and stays healthy: %+v", h)
	}
	r = models.NewWarmupPlacementRate(70, 0, 30)
	applyWarmupPlacement(&h, &r)
	if h.Score != 70 || h.Status != "warning" || len(h.Issues) != 1 {
		t.Fatalf("70%% warns: %+v", h)
	}
}
