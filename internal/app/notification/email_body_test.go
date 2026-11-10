package notification

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestEmailBodyLeavesAnInboundSubjectOut(t *testing.T) {
	subject := "Claim your prize at evil.example"
	for _, cat := range []models.NotificationCategory{models.NotifInboundReply, models.NotifInboundPositiveReply, models.NotifInboundOOO} {
		if got := emailBody(models.Notification{Category: cat, Body: subject}); strings.Contains(got, subject) {
			t.Errorf("%s: email body carries the sender's subject", cat)
		}
	}
	if got := emailBody(models.Notification{Category: models.NotifCampaignPaused, Body: "Bounce rate above 5%"}); got != "Bounce rate above 5%" {
		t.Errorf("other bodies changed: %q", got)
	}
}
