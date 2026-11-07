package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// scopedUnibox answers which mailboxes a thread sits in and counts every write that reaches it.
type scopedUnibox struct {
	unibox.UniboxService
	threads map[string][]uuid.UUID
	writes  int
}

func (f *scopedUnibox) MessageMailboxes(_ context.Context, _ uuid.UUID, _ []uuid.UUID, threadIDs []string) ([]uuid.UUID, *errx.Error) {
	var out []uuid.UUID
	for _, t := range threadIDs {
		out = append(out, f.threads[t]...)
	}
	return out, nil
}

func (f *scopedUnibox) LatestMessageIDInThread(context.Context, uuid.UUID, string) (string, *errx.Error) {
	return "", errx.New(errx.NotFound, "none")
}

func (f *scopedUnibox) SetThreadLabels(context.Context, uuid.UUID, uuid.UUID, string, []uuid.UUID) ([]models.MiniCategory, *errx.Error) {
	f.writes++
	return nil, nil
}

func (f *scopedUnibox) MarkSeenBulk(_ context.Context, _ uuid.UUID, d *models.MarkSeen) (*models.MarkSeen, *errx.Error) {
	f.writes++
	return d, nil
}

func (f *scopedUnibox) MoveFolderBulk(_ context.Context, _ uuid.UUID, d *models.MoveFolder) (*models.MoveFolder, *errx.Error) {
	f.writes++
	return d, nil
}

func (f *scopedUnibox) Snooze(context.Context, uuid.UUID, uuid.UUID, []string, time.Time) ([]models.UniboxSnooze, *errx.Error) {
	f.writes++
	return nil, nil
}

type scopedSend struct{ sent int }

func (f *scopedSend) SendEmail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *emailsend.SendEmailRequest) (*emailsend.SendEmailResponse, *errx.Error) {
	f.sent++
	return &emailsend.SendEmailResponse{SendMode: "instant"}, nil
}

type scopedCampaigns struct {
	campaign.CampaignService
	strategy string
	tags     []string
	senders  []uuid.UUID
	writes   int
}

func (f *scopedCampaigns) Update(context.Context, string, string, *models.UpdateCampaign) (*models.Campaign, *errx.Error) {
	f.writes++
	return &models.Campaign{}, nil
}

func (f *scopedCampaigns) Get(context.Context, string, string) (*models.Campaign, *errx.Error) {
	return &models.Campaign{SenderStrategy: f.strategy, EmailTags: f.tags}, nil
}

func (f *scopedCampaigns) ListCampaignSenders(context.Context, uuid.UUID, string) ([]models.CampaignSender, *errx.Error) {
	out := make([]models.CampaignSender, 0, len(f.senders))
	for _, id := range f.senders {
		out = append(out, models.CampaignSender{EmailAccountID: id, Enabled: true})
	}
	return out, nil
}

func (f *scopedCampaigns) StartCampaign(context.Context, uuid.UUID, string, models.StartCampaignOptions) *errx.Error {
	f.writes++
	return nil
}

type scopedSteps struct {
	sequence.SequenceService
	writes int
}

func (f *scopedSteps) Update(context.Context, string, string, string, *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	f.writes++
	return &models.Sequence{}, nil
}

// serveRestricted runs one request as a restricted member whose grants are mailboxes.
func serveRestricted(t *testing.T, mailboxes []uuid.UUID, method, route, path, body string, handle gin.HandlerFunc) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	orgID, userID := uuid.New(), uuid.New()
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
		c.Set(middleware.UserIDKey, userID.String())
		c.Set(middleware.OrganizationIDKey, orgID)
		c.Set(middleware.AuthTypeKey, middleware.AuthTypeJWT)
		c.Set(middleware.SessionMemberKey, &models.OrganizationMember{OrganizationID: orgID, UserID: userID, Role: "Client",
			Permissions: models.ScopeActionPermissions, AccessScope: models.AccessScopeRestricted})
		c.Set(middleware.ResourceScopeKey, &models.ResourceScope{Mailboxes: mailboxes, Actions: models.ScopeActionPermissions})
	})
	r.Handle(method, route, handle)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

// A restricted member's inbox writes reach only conversations held entirely in their granted mailboxes.
func TestRestrictedUniboxWritesStayInGrantedMailboxes(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	threads := map[string][]uuid.UUID{"mine": {granted}, "theirs": {other}, "shared": {granted, other}}
	for _, tc := range []struct {
		name, method, route, body string
		call                      func(h *Handler) gin.HandlerFunc
	}{
		{"labels", http.MethodPut, "/unibox/thread/labels", `{"thread_id":"%s","category_ids":[]}`, func(h *Handler) gin.HandlerFunc { return h.SetUniboxThreadLabels }},
		{"seen", http.MethodPatch, "/unibox/seen", `{"thread_ids":["%s"],"seen":true}`, func(h *Handler) gin.HandlerFunc { return h.UniboxMarkSeen }},
		{"folder", http.MethodPatch, "/unibox/folder", `{"thread_ids":["%s"],"folder":"archive"}`, func(h *Handler) gin.HandlerFunc { return h.UniboxMoveFolder }},
		{"snooze", http.MethodPost, "/unibox/snooze", `{"thread_id":"%s","snoozed_until":"2099-01-01T00:00:00Z"}`, func(h *Handler) gin.HandlerFunc { return h.CreateUniboxSnooze }},
	} {
		for thread, allowed := range map[string]bool{"mine": true, "theirs": false, "shared": false} {
			t.Run(tc.name+"/"+thread, func(t *testing.T) {
				ub := &scopedUnibox{threads: threads}
				h := &Handler{UniboxService: ub, AuditService: audit.NewNoOpService()}
				code := serveRestricted(t, []uuid.UUID{granted}, tc.method, tc.route, tc.route, strings.Replace(tc.body, "%s", thread, 1), tc.call(h))
				if allowed != (code == http.StatusOK) || allowed != (ub.writes == 1) {
					t.Fatalf("status %d, writes %d, want allowed=%v", code, ub.writes, allowed)
				}
			})
		}
	}
}

// A restricted member's reply sends from a granted mailbox into a conversation held in granted mailboxes.
func TestRestrictedReplyNeedsAGrantedConversation(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	threads := map[string][]uuid.UUID{"mine": {granted}, "theirs": {other}}
	for _, tc := range []struct {
		name   string
		from   uuid.UUID
		thread string
		sends  bool
	}{
		{"granted conversation", granted, "mine", true},
		{"another mailbox's conversation", granted, "theirs", false},
		{"a conversation that does not exist", granted, "made-up", false},
		{"no conversation", granted, "", false},
		{"from a mailbox outside the grants", other, "mine", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			send := &scopedSend{}
			h := &Handler{UniboxService: &scopedUnibox{threads: threads}, EmailSendService: send, AuditService: audit.NewNoOpService()}
			body := `{"email_account_id":"` + tc.from.String() + `","to":["lead@example.org"],"subject":"Re: hi","thread_id":"` + tc.thread + `"}`
			code := serveRestricted(t, []uuid.UUID{granted}, http.MethodPost, "/unibox/reply", "/unibox/reply", body, h.UniboxReply)
			if tc.sends != (send.sent == 1) || tc.sends != (code == http.StatusOK) {
				t.Fatalf("status %d, sent %d, want sends=%v", code, send.sent, tc.sends)
			}
		})
	}
}

// A restricted member edits a granted campaign's own settings, never the workspace resources it points at.
func TestRestrictedCampaignEditsStayInTheCampaign(t *testing.T) {
	for body, ok := range map[string]bool{
		`{"name":"Q3 outreach","days":31}`: true,
		`{"status":"active"}`:              false,
		`{"email_tags":["x"]}`:             false,
		`{"sender_strategy":"tags"}`:       false,
		`{"folders":["x"]}`:                false,
		`{"tracking_domain":"t.example"}`:  false,
		`{"bcc":["[EMAIL]"]}`:              false,
		`{"cc":["[EMAIL]"]}`:               false,
	} {
		granted := uuid.New()
		svc := &scopedCampaigns{strategy: "explicit", senders: []uuid.UUID{granted}}
		h := &Handler{CampaignService: svc, AuditService: audit.NewNoOpService()}
		id := uuid.NewString()
		code := serveRestricted(t, []uuid.UUID{granted}, http.MethodPatch, "/campaigns/:id", "/campaigns/"+id, body, h.UpdateCampaign)
		if ok != (svc.writes == 1) || ok != (code == http.StatusOK) {
			t.Fatalf("%s: status %d, writes %d, want ok=%v", body, code, svc.writes, ok)
		}
	}
}

func TestRestrictedStepEditsStayInTheCampaign(t *testing.T) {
	for body, ok := range map[string]bool{
		`{"subject":"Following up"}`:                                   true,
		`{"kind":"action","action":{"type":"wait","wait_minutes":60}}`: true,
		`{"action":{"type":"create_deal"}}`:                            false,
		`{"action":{"type":"run_automation"}}`:                         false,
		`{"kind":"action"}`:                                            false,
	} {
		granted := uuid.New()
		steps := &scopedSteps{}
		h := &Handler{SequenceService: steps, CampaignService: &scopedCampaigns{strategy: "explicit", senders: []uuid.UUID{granted}}, AuditService: audit.NewNoOpService()}
		code := serveRestricted(t, []uuid.UUID{granted}, http.MethodPatch, "/campaigns/:id/steps/:sid", "/campaigns/"+uuid.NewString()+"/steps/"+uuid.NewString(), body, h.UpdateSequence)
		if ok != (steps.writes == 1) || ok != (code == http.StatusOK) {
			t.Fatalf("%s: status %d, writes %d, want ok=%v", body, code, steps.writes, ok)
		}
	}
}

// Starting needs every sender to be a granted mailbox, listed explicitly.
func TestRestrictedStartSendsOnlyFromGrantedMailboxes(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name     string
		strategy string
		tags     []string
		senders  []uuid.UUID
		starts   bool
	}{
		{"explicit granted senders", "explicit", nil, []uuid.UUID{granted}, true},
		{"a sender outside the grants", "explicit", nil, []uuid.UUID{granted, other}, false},
		{"a tag pool", "tags", []string{"x"}, nil, false},
		{"explicit senders with tags still on the campaign", "explicit", []string{"x"}, []uuid.UUID{granted}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &scopedCampaigns{strategy: tc.strategy, tags: tc.tags, senders: tc.senders}
			h := &Handler{CampaignService: svc, AuditService: audit.NewNoOpService()}
			code := serveRestricted(t, []uuid.UUID{granted}, http.MethodPost, "/campaigns/:id/start", "/campaigns/"+uuid.NewString()+"/start", "", h.StartCampaign)
			if tc.starts != (svc.writes == 1) {
				t.Fatalf("status %d, starts %d, want starts=%v", code, svc.writes, tc.starts)
			}
		})
	}
}

// Campaign and step edits reach only campaigns that send from granted mailboxes alone.
func TestRestrictedEditsNeedAGrantedPool(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name string
		svc  *scopedCampaigns
		ok   bool
	}{
		{"granted explicit pool", &scopedCampaigns{strategy: "explicit", senders: []uuid.UUID{granted}}, true},
		{"a sender outside the grants", &scopedCampaigns{strategy: "explicit", senders: []uuid.UUID{granted, other}}, false},
		{"a tag pool", &scopedCampaigns{strategy: "tags", tags: []string{"x"}}, false},
		{"explicit senders with tags still on the campaign", &scopedCampaigns{strategy: "explicit", tags: []string{"x"}, senders: []uuid.UUID{granted}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := &scopedSteps{}
			h := &Handler{CampaignService: tc.svc, SequenceService: steps, AuditService: audit.NewNoOpService()}
			id := uuid.NewString()
			edit := serveRestricted(t, []uuid.UUID{granted}, http.MethodPatch, "/campaigns/:id", "/campaigns/"+id, `{"name":"Q3"}`, h.UpdateCampaign)
			step := serveRestricted(t, []uuid.UUID{granted}, http.MethodPatch, "/campaigns/:id/steps/:sid", "/campaigns/"+id+"/steps/"+uuid.NewString(), `{"subject":"Hi"}`, h.UpdateSequence)
			if tc.ok != (tc.svc.writes == 1) || tc.ok != (steps.writes == 1) || tc.ok != (edit == http.StatusOK) || tc.ok != (step == http.StatusOK) {
				t.Fatalf("edit %d (writes %d), step %d (writes %d), want ok=%v", edit, tc.svc.writes, step, steps.writes, tc.ok)
			}
		})
	}
}
