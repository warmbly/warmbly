package advanced

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// holdRecorder captures the one call holdForOutOfOffice makes. Embedding the
// interface beats stubbing forty methods; anything else the code reached for
// would panic rather than pass quietly.
type holdRecorder struct {
	repository.CampaignProgressRepository
	until  *time.Time
	reason string
	source string
	calls  int
	refuse bool
}

func (h *holdRecorder) HoldLead(_ context.Context, _, _ uuid.UUID, until *time.Time, reason, source string) (*models.LeadHold, error) {
	h.calls++
	h.until, h.reason, h.source = until, reason, source
	if h.refuse {
		return nil, nil
	}
	return &models.LeadHold{Since: time.Now(), Until: until, Reason: reason, Source: source}, nil
}

// An out-of-office reply parks the contact until they are back: the date the
// away message names, plus a working day so the follow-up does not land in the
// backlog of their first morning. Without a readable date the workspace's
// fallback applies instead of nothing at all.
func TestHoldForOutOfOffice(t *testing.T) {
	cfg := models.ReplyIntentSettings{HoldOnOutOfOffice: true, OutOfOfficeHoldDays: 7}
	// Dates are written relative to today, so the assertions do not expire.
	// A week out is inside the parser's sanity window and safely in the future
	// whatever day the suite runs on.
	back := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 7)
	cases := []struct {
		name       string
		subject    string
		body       string
		wantDay    string // "" = the fallback hold
		wantReason string
	}{
		{
			name:       "an ISO return date, plus a working day",
			subject:    "Automatic reply: Quick question",
			body:       "I am away and back on " + back.Format("2006-01-02") + ".",
			wantDay:    replyclassify.NextBusinessDay(back).Format("2006-01-02"),
			wantReason: "back " + back.Format("2 Jan 2006"),
		},
		{
			name:       "a German day-dot-month return date",
			subject:    "Abwesenheitsnotiz",
			body:       "Ich bin wieder erreichbar am " + back.Format("02.01.2006") + ".",
			wantDay:    replyclassify.NextBusinessDay(back).Format("2006-01-02"),
			wantReason: "back " + back.Format("2 Jan 2006"),
		},
		{
			name:       "no readable date falls back",
			subject:    "Automatische Antwort: Ihre Nachricht",
			body:       "Ich bin zur Zeit nicht im Büro.",
			wantReason: "auto-reply, no return date",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &holdRecorder{}
			s := &service{campaignProgressRepo: rec}
			got := s.holdForOutOfOffice(context.Background(), uuid.New(), uuid.New(),
				cfg, &models.EmailMessageStoreData{Subject: tc.subject, BodyText: tc.body})

			if rec.calls != 1 {
				t.Fatalf("HoldLead called %d times, want once", rec.calls)
			}
			if rec.source != models.LeadHoldSourceOutOfOffice {
				t.Fatalf("hold source = %q, want %q", rec.source, models.LeadHoldSourceOutOfOffice)
			}
			if rec.reason != tc.wantReason {
				t.Fatalf("hold reason = %q, want %q", rec.reason, tc.wantReason)
			}
			if rec.until == nil || got == nil {
				t.Fatal("no hold end written; the follow-up would keep its schedule")
			}
			if tc.wantDay == "" {
				// The fallback: seven days out, give or take the clock.
				if d := time.Until(*rec.until); d < 6*24*time.Hour || d > 7*24*time.Hour+time.Minute {
					t.Fatalf("fallback hold lifts in %s, want about seven days", d.Round(time.Hour))
				}
				return
			}
			if day := rec.until.UTC().Format("2006-01-02"); day != tc.wantDay {
				t.Fatalf("hold lifts on %s, want %s", day, tc.wantDay)
			}
		})
	}
}

// A hold the repository refuses (a member's own pause already there) reports
// nothing, so the notification does not promise a hold that was not written.
func TestHoldForOutOfOfficeReportsARefusal(t *testing.T) {
	rec := &holdRecorder{refuse: true}
	s := &service{campaignProgressRepo: rec}
	if got := s.holdForOutOfOffice(context.Background(), uuid.New(), uuid.New(),
		models.ReplyIntentSettings{HoldOnOutOfOffice: true, OutOfOfficeHoldDays: 7},
		&models.EmailMessageStoreData{Subject: "Out of office", BodyText: "away"}); got != nil {
		t.Fatalf("holdForOutOfOffice = %v after a refused hold, want nil", got)
	}
}

// A workspace whose stored settings predate the setting carries a zero here.
// Read as "resume immediately" it would send into the away message it was
// triggered by, so the floor applies.
func TestHoldForOutOfOfficeFloorsAZeroFallback(t *testing.T) {
	rec := &holdRecorder{}
	s := &service{campaignProgressRepo: rec}
	s.holdForOutOfOffice(context.Background(), uuid.New(), uuid.New(),
		models.ReplyIntentSettings{HoldOnOutOfOffice: true},
		&models.EmailMessageStoreData{Subject: "Out of office", BodyText: "away"})
	if rec.until == nil || !rec.until.After(time.Now().Add(23*time.Hour)) {
		t.Fatalf("hold lifts at %v, want at least a day out", rec.until)
	}
}
