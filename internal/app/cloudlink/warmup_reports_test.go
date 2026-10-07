package cloudlink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupReportRepo struct {
	repository.CloudLinkRepository
	link      *models.CloudLink
	org       uuid.UUID
	mailboxes []models.CloudLinkMailbox
}

func (r warmupReportRepo) GetByInstance(context.Context, uuid.UUID) (*models.CloudLink, error) {
	return r.link, nil
}
func (r warmupReportRepo) ListForOrg(_ context.Context, org uuid.UUID, id *uuid.UUID) ([]models.CloudLinkMailbox, error) {
	out := make([]models.CloudLinkMailbox, 0)
	if org == r.org {
		for _, mailbox := range r.mailboxes {
			if id == nil || mailbox.EmailAccountID == *id {
				out = append(out, mailbox)
			}
		}
	}
	return out, nil
}

func TestWarmupReportsBatchAndRemapExistingEnrollments(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	org := uuid.New()
	var mailboxes []models.CloudLinkMailbox
	for i := 0; i < 101; i++ {
		mailboxes = append(mailboxes, models.CloudLinkMailbox{EmailAccountID: uuid.New(), RemoteID: uuid.New(), Managed: i%2 == 0})
	}
	statsCalls, placementCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("unexpected request method or authorization")
		}
		var req models.PoolLinkWarmupReportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if _, _, err := req.Range(); err != nil {
			t.Error(err)
			return
		}
		switch r.URL.Path {
		case "/v1/pool-link/instance/analytics/warmup":
			statsCalls++
			_ = json.NewEncoder(w).Encode([]models.WarmupDailyStats{{Date: req.From, EmailsSent: len(req.RemoteIDs), Active: true}})
		case "/v1/pool-link/instance/analytics/warmup/placement":
			placementCalls++
			data := models.WarmupPlacementData{Hosts: []models.WarmupPlacementHostRow{}, Windows: map[uuid.UUID]models.WarmupPlacementWindow{}}
			for _, id := range req.RemoteIDs {
				data.Daily = append(data.Daily, models.WarmupPlacementDayRow{SenderID: id, Date: req.From, Inbox: 1})
				data.Sent = append(data.Sent, models.WarmupSenderDayCount{SenderID: id, Count: 1})
				data.Unconfirmed = append(data.Unconfirmed, models.WarmupSenderDayCount{SenderID: id, Count: 1})
				data.Windows[id] = models.WarmupPlacementWindow{Major: models.WarmupPlacementTally{Inbox: 1}}
			}
			_ = json.NewEncoder(w).Encode(data)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := &service{repo: warmupReportRepo{link: &models.CloudLink{CloudURL: srv.URL, Token: "test"}, org: org, mailboxes: mailboxes}}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, xerr := s.WarmupStats(context.Background(), org, nil, from, from.AddDate(0, 0, 366))
	if xerr != nil || statsCalls != 4 || len(stats) != 2 || stats[0].EmailsSent != 101 || stats[1].EmailsSent != 101 || !stats[0].Active {
		t.Fatalf("stats=%+v error=%v calls=%d", stats, xerr, statsCalls)
	}
	data, xerr := s.WarmupPlacementData(context.Background(), org, nil, from, from)
	if xerr != nil || placementCalls != 11 || len(data.Daily) != 101 || len(data.Windows) != 101 {
		t.Fatalf("placement error=%v calls=%d data=%+v", xerr, placementCalls, data)
	}
	for i, m := range mailboxes {
		if data.Daily[i].SenderID != m.EmailAccountID || data.Sent[i].SenderID != m.EmailAccountID || data.Unconfirmed[i].SenderID != m.EmailAccountID || data.Windows[m.EmailAccountID].Major.Inbox != 1 {
			t.Fatalf("remote IDs were not mapped: %+v", data.Daily[i])
		}
	}
	before := statsCalls
	if rows, xerr := s.WarmupStats(context.Background(), uuid.New(), nil, from, from); xerr != nil || len(rows) != 0 || statsCalls != before {
		t.Fatalf("another workspace reached Cloud: %v", xerr)
	}
	if _, xerr := s.WarmupStats(context.Background(), org, &mailboxes[0].EmailAccountID, from, from); xerr != nil || statsCalls != before+1 {
		t.Fatalf("single mailbox report: %v", xerr)
	}
}

func TestWarmupReportsDoNotTurnCloudFailuresIntoEmptyActivity(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"old cloud", 404, "{}"}, {"outage", 503, "{}"}, {"revoked", 401, "{}"},
		{"invalid JSON", 200, "bad"}, {"empty response", 200, ""}, {"null response", 200, "null"},
		{"missing fields", 200, "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			org, local := uuid.New(), uuid.New()
			s := &service{repo: warmupReportRepo{link: &models.CloudLink{CloudURL: srv.URL}, org: org, mailboxes: []models.CloudLinkMailbox{{EmailAccountID: local, RemoteID: uuid.New()}}}}
			day := time.Now().UTC()
			if _, xerr := s.WarmupStats(context.Background(), org, &local, day, day); xerr == nil {
				t.Fatal("stats failure was swallowed")
			}
			if _, xerr := s.WarmupPlacementData(context.Background(), org, &local, day, day); xerr == nil {
				t.Fatal("placement failure was swallowed")
			}
		})
	}
}
