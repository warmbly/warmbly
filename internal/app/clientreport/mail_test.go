package clientreport

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/models"
)

func TestReportEmailInterpolatesOnlyDisplaySafeNames(t *testing.T) {
	rep := &models.ClientReport{Name: "Visit evil.example now"}
	positive := 4
	view := &models.ClientReportView{From: "2026-11-01", To: "2026-11-07",
		Overview:  &models.ClientReportOverview{Sent: 12345, Replied: 40, ReplyRate: 0.32, Positive: &positive, BounceRate: 1.25},
		Campaigns: []models.ClientReportCampaign{{Name: "https://phish.example", Sent: 10}, {Name: "<b>Dentists</b>", Sent: 5}}}
	brand := brandkit.Brand{Name: "Acme <script>", LogoURL: "javascript:alert(1)", AccentColor: "red;background:url(x)"}
	subject, html, err := reportEmail(rep, view, brand, "https://forms.example.com/r/token")
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Your report: Nov 1 to Nov 7, 2026" {
		t.Fatalf("subject = %q", subject)
	}
	for _, leaked := range []string{"evil.example", "phish.example", "<script>", "javascript:", "url(x)", "<b>Dentists"} {
		if strings.Contains(html, leaked) {
			t.Errorf("email contains %q", leaked)
		}
	}
	for _, want := range []string{"Your team", "A campaign", "12,345", "https://forms.example.com/r/token", "View full report"} {
		if !strings.Contains(html, want) {
			t.Errorf("email lacks %q", want)
		}
	}
}
