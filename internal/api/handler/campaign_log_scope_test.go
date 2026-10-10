package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestScopedCampaignLogMixedMailboxPools(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	rows := []map[string]interface{}{
		{"mailbox": "granted@example.test", "cap": 15, "sent_today": 12, "limited_by": "campaign_ramp", "warmup_health": "watch", "sending_now": true, "gate": "budget", "new_details": []interface{}{map[string]interface{}{"error": "private-provider-error"}}},
		{"mailbox": "private@example.test", "cap": 777, "sent_today": 666, "warmup_health": "blocked"},
		{"mailbox": "granted@example.test", "account_id": other.String(), "cap": 999},
		{"mailbox": "granted@example.test", "cap": map[string]interface{}{"private": "private@example.test"}, "sent_today": []interface{}{"private@example.test"}, "warmup_health": "private-provider-error"},
	}
	for _, event := range []string{"daily_cap_reached", "mailboxes_unavailable", "mailboxes_send_recovery"} {
		for _, decoded := range []bool{false, true} {
			t.Run(event+map[bool]string{false: "/typed", true: "/json"}[decoded], func(t *testing.T) {
				entry := models.CampaignLog{ID: uuid.New(), CampaignID: uuid.New(), EventType: event,
					Message:  "Private pool: private@example.test cap 777",
					Metadata: map[string]interface{}{"mailboxes": rows, "pool_size": 3, "capped_mailboxes": 2, "cap": 777, "sent_today": 666, "health_held": 1, "resting_mailboxes": 1, "auth_gated": 1, "hours_closed": 1, "resumes_at": "private-provider-error"},
				}
				if decoded {
					entry = roundTripCampaignLog(t, entry)
				}
				before, _ := json.Marshal(entry)
				got := scopedCampaignLog(entry, []uuid.UUID{granted}, map[string]uuid.UUID{"granted@example.test": granted})
				assertCampaignLogOmits(t, got, "private@example.test", "private-provider-error", other.String(), `"cap":777`, `"sent_today":666`, `"cap":999`, "pool_size", "capped_mailboxes", "health_held", "resumes_at")
				kept := got.Metadata["mailboxes"].([]map[string]interface{})
				if len(kept) != 2 || len(kept[0]) != 7 || len(kept[1]) != 1 {
					t.Fatalf("scoped rows = %+v, want authorized scalars only", kept)
				}
				if kept[0]["mailbox"] != "granted@example.test" || kept[0]["limited_by"] != "campaign_ramp" || kept[0]["warmup_health"] != "watch" {
					t.Errorf("granted budget context lost: %+v", kept[0])
				}
				after, _ := json.Marshal(entry)
				if string(before) != string(after) || got.ID != entry.ID || got.CampaignID != entry.CampaignID {
					t.Fatal("redaction changed stored data or log identity")
				}
			})
		}
	}
}

func TestScopedCampaignLogMailboxIdentitiesAndProviderErrors(t *testing.T) {
	granted, other, contact, sequence, task := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, event := range []string{"email_sent", "email_failed", "sender_reassigned", "progress_write_failed"} {
		for _, account := range []uuid.UUID{granted, other} {
			t.Run(event+"/"+account.String(), func(t *testing.T) {
				entry := roundTripCampaignLog(t, models.CampaignLog{EventType: event, Message: "private@example.test reassigned to granted@example.test",
					Metadata: map[string]interface{}{"account_id": account, "prev_account_id": other, "contact_id": contact, "sequence_id": sequence, "task_id": task, "error": "private-provider-error", "code": "SEND_FAILED", "will_retry": true, "attempts": 1, "max_attempts": 3, "cap": 444, "extra": map[string]interface{}{"account_id": other}},
				})
				got := scopedCampaignLog(entry, []uuid.UUID{granted}, nil)
				assertCampaignLogOmits(t, got, other.String(), "private-provider-error", "private@example.test", `"cap"`, "prev_account_id")
				if (got.Metadata["account_id"] == granted.String()) != (account == granted) {
					t.Errorf("account grant not enforced: %+v", got.Metadata)
				}
				if got.Metadata["contact_id"] != contact.String() || got.Metadata["sequence_id"] != sequence.String() || got.Metadata["task_id"] != task.String() {
					t.Errorf("campaign context lost: %+v", got.Metadata)
				}
				if event == "email_failed" && (got.Metadata["code"] != "SEND_FAILED" || got.Metadata["will_retry"] != true) {
					t.Errorf("safe failure context lost: %+v", got.Metadata)
				}
			})
		}
	}
	entry := models.CampaignLog{EventType: "sender_reassigned", Metadata: map[string]interface{}{"account_id": other.String(), "prev_account_id": granted.String()}}
	if got := scopedCampaignLog(entry, []uuid.UUID{granted}, nil); got.Metadata["prev_account_id"] != granted.String() {
		t.Fatalf("authorized previous sender lost: %+v", got.Metadata)
	}
}

func TestScopedCampaignLogAddressOnlyEvents(t *testing.T) {
	granted := uuid.New()
	for _, event := range []string{"sender_busy", "tracking_domain_unverified"} {
		for _, address := range []string{"granted@example.test", "private@example.test"} {
			entry := models.CampaignLog{EventType: event, Message: "private@example.test using private-tracking.example.test",
				Metadata: map[string]interface{}{"mailbox": address, "tracking_domain": "private-tracking.example.test", "scope": "mailbox", "reason": "budget", "error": "private-provider-error", "cap": 999, "sent_today": 999, "warmup_health": "blocked"},
			}
			got := scopedCampaignLog(entry, []uuid.UUID{granted}, map[string]uuid.UUID{"granted@example.test": granted})
			assertCampaignLogOmits(t, got, "private@example.test", "private-tracking.example.test", "private-provider-error", `"cap"`, `"sent_today"`, "warmup_health")
			if (got.Metadata["mailbox"] == "granted@example.test") != (address == "granted@example.test") {
				t.Errorf("%s address grant not enforced: %+v", event, got.Metadata)
			}
		}
	}
}

func TestScopedCampaignLogUnknownAndMalformedMetadataFailsClosed(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	for _, event := range []string{"future_event", "future-private@example.test", "email_failed", "guardrail_paused", "content_warning", "sender_busy"} {
		entry := models.CampaignLog{EventType: event, Message: "private@example.test",
			Metadata: map[string]interface{}{"level": "private@example.test", "code": "private@example.test", "rule": "private@example.test", "reason": "private@example.test", "scope": "private@example.test", "contact_id": other.String() + " private@example.test", "account_id": []interface{}{granted.String(), other.String()}, "score": map[string]interface{}{"private": "private@example.test"}, "mailbox": "private@example.test", "mailboxes": []interface{}{map[string]interface{}{"mailbox": "private@example.test"}}, "future_field": []interface{}{map[string]interface{}{"deep": []interface{}{"private@example.test", other.String()}}}},
		}
		got := scopedCampaignLog(entry, []uuid.UUID{granted}, nil)
		assertCampaignLogOmits(t, got, "private@example.test", other.String(), granted.String())
		if len(got.Metadata) != 0 {
			t.Fatalf("%s unsafe metadata kept: %+v", event, got.Metadata)
		}
	}
	for event := range scopedCampaignLogMessages {
		entry := models.CampaignLog{EventType: event, Message: "private@example.test", Metadata: map[string]interface{}{"nested": map[string]interface{}{"mailbox": "private@example.test"}, "list": []interface{}{other.String()}, "error": "private-provider-error", "mailbox": "private@example.test", "account_id": other.String(), "prev_account_id": other.String(), "cap": 999, "sent_today": 999, "tracking_domain": "private-tracking.example.test"}}
		got := scopedCampaignLog(entry, []uuid.UUID{}, map[string]uuid.UUID{"private@example.test": other})
		assertCampaignLogOmits(t, got, "private@example.test", "private-provider-error", other.String(), `"cap":999`, `"sent_today":999`, "private-tracking.example.test")
	}
}

func TestScopedCampaignLogUnrestrictedCompatibility(t *testing.T) {
	for _, event := range []string{"future_event", "daily_cap_reached", "email_failed"} {
		entry := models.CampaignLog{EventType: event, Message: "Original provider message", Metadata: map[string]interface{}{"unknown": []interface{}{"private@example.test"}, "error": "provider failure", "mailboxes": []interface{}{map[string]interface{}{"mailbox": "private@example.test"}}}}
		if got := scopedCampaignLog(entry, nil, nil); !reflect.DeepEqual(got, entry) {
			t.Errorf("unrestricted log changed: %+v", got)
		}
	}
}

type scopedCampaignLogServiceStub struct {
	campaign.CampaignService
	logs              []models.CampaignLog
	orgID, campaignID uuid.UUID
	t                 *testing.T
}

func (s scopedCampaignLogServiceStub) GetLogs(_ context.Context, orgID, campaignID string, limit int, cursor *string) (*models.CampaignLogsResult, *errx.Error) {
	if orgID != s.orgID.String() || campaignID != s.campaignID.String() || limit != 50 || cursor != nil {
		s.t.Fatalf("unexpected campaign log request %s/%s limit=%d cursor=%v", orgID, campaignID, limit, cursor)
	}
	next := "next-log-page"
	return &models.CampaignLogsResult{Data: append([]models.CampaignLog{}, s.logs...), Pagination: models.CPagination{HasMore: true, NextCursor: &next}}, nil
}

type scopedCampaignLogEmailStub struct {
	email.EmailService
	t       *testing.T
	orgID   uuid.UUID
	allowed []uuid.UUID
	pages   map[string]*models.EmailsResult
	fail    bool
	calls   int
}

func (s *scopedCampaignLogEmailStub) Search(_ context.Context, orgID, search, cursor, tag, limit string, allowed []uuid.UUID) (*models.EmailsResult, *errx.Error) {
	s.calls++
	if orgID != s.orgID.String() || search != "" || tag != "" || limit != "100" || !reflect.DeepEqual(allowed, s.allowed) {
		s.t.Fatalf("mailbox lookup must be org/grant scoped: %s allowed=%v", orgID, allowed)
	}
	if s.fail {
		return nil, errx.New(errx.Internal, "lookup failed")
	}
	return s.pages[cursor], nil
}

func TestGetCampaignLogsUsesMailboxGrantsAndPreservesPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orgID, campaignID, granted, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	entry := models.CampaignLog{ID: uuid.New(), CampaignID: campaignID, EventType: "daily_cap_reached", Message: "private@example.test",
		Metadata: map[string]interface{}{"mailboxes": []map[string]interface{}{{"mailbox": "granted@example.test", "cap": 10, "sent_today": 8}, {"mailbox": "private@example.test", "cap": 999}}, "pool_size": 2},
	}
	for _, tc := range []struct {
		name                string
		allowed             []uuid.UUID
		restricted, failed  bool
		wantRows, wantCalls int
	}{
		{"restricted grants", []uuid.UUID{granted}, true, false, 1, 2},
		{"empty grants", []uuid.UUID{}, true, false, 0, 0},
		{"failed grant lookup", []uuid.UUID{granted}, true, true, 0, 1},
		{"unrestricted", nil, false, false, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed := tc.allowed
			if tc.restricted && len(allowed) == 0 {
				allowed = models.NoneMatch()
			}
			next := "next-mailbox-page"
			mailboxes := &scopedCampaignLogEmailStub{t: t, orgID: orgID, allowed: allowed, fail: tc.failed, pages: map[string]*models.EmailsResult{
				"":   {Data: []models.Email{{ID: other, Email: "private@example.test"}}, Pagination: models.Pagination{HasMore: true, NextCursor: &next}},
				next: {Data: []models.Email{{ID: granted, Email: "granted@example.test"}}},
			}}
			h := &Handler{CampaignService: scopedCampaignLogServiceStub{logs: []models.CampaignLog{entry}, orgID: orgID, campaignID: campaignID, t: t}, EmailService: mailboxes}
			router := gin.New()
			router.GET("/campaigns/:id/logs", func(c *gin.Context) {
				c.Set(middleware.OrganizationIDKey, orgID)
				if tc.restricted {
					c.Set(middleware.ResourceScopeKey, &models.ResourceScope{Mailboxes: tc.allowed, Campaigns: []uuid.UUID{campaignID}})
				}
				h.GetCampaignLogs(c)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/campaigns/"+campaignID.String()+"/logs", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var result models.CampaignLogsResult
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Data) != 1 || len(campaignLogMailboxRows(result.Data[0].Metadata["mailboxes"])) != tc.wantRows || mailboxes.calls != tc.wantCalls {
				t.Fatalf("rows=%+v lookup calls=%d, want %d/%d", result.Data, mailboxes.calls, tc.wantRows, tc.wantCalls)
			}
			if !result.Pagination.HasMore || result.Pagination.NextCursor == nil || *result.Pagination.NextCursor != "next-log-page" {
				t.Fatalf("log pagination changed: %+v", result.Pagination)
			}
			if tc.restricted {
				assertCampaignLogOmits(t, result.Data[0], "private@example.test", `"cap":999`, other.String(), "pool_size")
			}
		})
	}
}

func TestGetCampaignLogsMissingResourceScopeFailsClosed(t *testing.T) {
	orgID, campaignID, mailboxID := uuid.New(), uuid.New(), uuid.New()
	entry := models.CampaignLog{EventType: "email_failed", Message: "private@example.test", Metadata: map[string]interface{}{"account_id": mailboxID.String(), "error": "private-provider-error", "code": "SEND_FAILED"}}
	h := &Handler{CampaignService: scopedCampaignLogServiceStub{logs: []models.CampaignLog{entry}, orgID: orgID, campaignID: campaignID, t: t}}
	router := gin.New()
	router.GET("/campaigns/:id/logs", func(c *gin.Context) {
		c.Set(middleware.OrganizationIDKey, orgID)
		c.Set(middleware.SessionMemberKey, &models.OrganizationMember{OrganizationID: orgID, AccessScope: models.AccessScopeRestricted})
		h.GetCampaignLogs(c)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/campaigns/"+campaignID.String()+"/logs", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result models.CampaignLogsResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 || result.Data[0].Metadata["code"] != "SEND_FAILED" {
		t.Fatalf("safe campaign context lost: %+v", result.Data)
	}
	assertCampaignLogOmits(t, result.Data[0], "private@example.test", "private-provider-error", mailboxID.String())
}

func roundTripCampaignLog(t *testing.T, entry models.CampaignLog) models.CampaignLog {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded models.CampaignLog
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func assertCampaignLogOmits(t *testing.T, entry models.CampaignLog, secrets ...string) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(data), secret) {
			t.Errorf("log leaked %q: %s", secret, data)
		}
	}
}
