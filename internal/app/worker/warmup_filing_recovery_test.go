package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

type filingBus struct {
	eventbus.EventBus
	events []models.JobEvent
	err    error
}

func (b *filingBus) Publish(_ context.Context, _, _ string, payload []byte) error {
	if b.err != nil {
		return b.err
	}
	var event models.JobEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	b.events = append(b.events, event)
	return nil
}

type filingIMAP struct {
	wmail.ImapConn
	found map[string]uint32
	moves int
	uid   uint32
	err   error
}

func (i *filingIMAP) FindUIDByMessageID(_ context.Context, folder, _ string) (uint32, error) {
	return i.found[folder], nil
}

func (i *filingIMAP) MoveToFolder(_ context.Context, source, destination string, uid uint32) (bool, error) {
	i.moves++
	i.uid = uid
	if i.err != nil {
		return false, i.err
	}
	delete(i.found, source)
	i.found[destination] = uid
	return true, nil
}

func filingWorker(id uuid.UUID, imap *filingIMAP) (*WorkerService, *filingBus) {
	w := newLoadingWorker(&capturedEvents{})
	w.mailManager.Emails[id] = &wmail.WMail{EmailType: models.InboxProviderSMTPIMAP,
		SmtpImapData: &wmail.SmtpImapData{ImapClient: imap, Mailboxes: []*models.Mailbox{{Name: "INBOX", UIDValidity: 77}}}}
	bus := &filingBus{}
	w.Bus, w.Codec = bus, codec.NewJSON()
	return w, bus
}

func TestDurableIMAPFilingRetriesFailureAndAcknowledgesIdempotently(t *testing.T) {
	account, id := uuid.New(), uuid.New()
	imap := &filingIMAP{found: map[string]uint32{"INBOX": 11}, err: errors.New("provider unavailable")}
	w, bus := filingWorker(account, imap)
	action := models.WarmupEmailAction{EmailID: account, FilingID: id.String(), RFCMessageID: "<warmup@example.test>",
		UID: 999, MailboxUIDValidity: 77, MailboxFolder: "INBOX", Actions: []string{models.WarmupActionFile}}
	if err := w.HandleWarmupAction(context.Background(), action); err == nil || len(bus.events) != 0 {
		t.Fatal("provider failure acknowledged or discarded the durable filing")
	}
	imap.err = nil
	if err := w.HandleWarmupAction(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	if imap.uid != 11 || len(bus.events) != 1 || bus.events[0].Type != models.JobEventTypeWarmupFiled {
		t.Fatalf("stale UID used or acknowledgement missing: uid=%d events=%+v", imap.uid, bus.events)
	}
	if err := w.HandleWarmupAction(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	if imap.moves != 2 || len(bus.events) != 2 {
		t.Fatal("retry after acknowledgement loss moved the already filed message again")
	}
	bus.err = errors.New("broker unavailable")
	if err := w.HandleWarmupAction(context.Background(), action); err == nil {
		t.Fatal("acknowledgement publish failure did not leave the filing retryable")
	}
}

func TestDisconnectFilesKnownWarmupBeforeDroppingMailboxEvenOnFailure(t *testing.T) {
	for _, failing := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "provider unavailable"}[failing], func(t *testing.T) {
			account := uuid.New()
			imap := &filingIMAP{found: map[string]uint32{"INBOX": 11}}
			if failing {
				imap.err = errors.New("provider unavailable")
			}
			w, _ := filingWorker(account, imap)
			if err := w.HandleRemoveEmail(context.Background(), &models.RemoveWorkerEmail{EmailID: account.String(), UserID: uuid.NewString(),
				WarmupMessageIDs: []string{"<warmup@example.test>"}, WarmupFolder: "Warmbly"}); err != nil {
				t.Fatal(err)
			}
			if imap.moves != 1 || w.mailManager.Has(account) {
				t.Fatalf("disconnect skipped cleanup or failed to drop mailbox: moves=%d loaded=%v", imap.moves, w.mailManager.Has(account))
			}
		})
	}
}

func TestDisconnectRespectsInboxPlacement(t *testing.T) {
	account := uuid.New()
	imap := &filingIMAP{found: map[string]uint32{"INBOX": 11}}
	w, _ := filingWorker(account, imap)
	if err := w.HandleRemoveEmail(context.Background(), &models.RemoveWorkerEmail{EmailID: account.String(),
		WarmupMessageIDs: []string{"<warmup@example.test>"}, WarmupPlacement: models.WarmupPlacementInbox}); err != nil {
		t.Fatal(err)
	}
	if imap.moves != 0 || w.mailManager.Has(account) {
		t.Fatal("disconnect moved deliberately inbox-placed warmup or kept the mailbox loaded")
	}
}

type filingTransport func(*http.Request) (*http.Response, error)

func (f filingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type warmupDeleteStore struct {
	storage.Store
	deletes int
	err     error
}

func (s *warmupDeleteStore) Delete(context.Context, string) error {
	s.deletes++
	return s.err
}

func TestGraphWarmupDeleteHoldsOnUnconfirmedLookup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"503 retry window", 503, `{"error":{"code":"Unavailable"}}`, nil},
		{"429 retry window", 429, `{"error":{"code":"Throttled"}}`, nil},
		{"lookup resource unavailable", 404, `{"error":{"code":"ErrorItemNotFound"}}`, nil},
		{"deadline", 0, "", context.DeadlineExceeded},
		{"ambiguous identity", 200, `{"value":[{"id":""}]}`, nil},
		{"malformed response", 200, `{`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failed, deletes := true, 0
			transport := filingTransport(func(r *http.Request) (*http.Response, error) {
				body, status := `{"value":[{"id":"resolved"}]}`, http.StatusOK
				h := make(http.Header)
				switch r.Method {
				case http.MethodGet:
					if !strings.HasSuffix(r.URL.Path, "/messages") {
						t.Fatalf("unexpected lookup: %s", r.URL.Path)
					}
					if failed {
						if tc.err != nil {
							return nil, tc.err
						}
						body, status = tc.body, tc.status
						h.Set("Retry-After", "600")
					}
				case http.MethodDelete:
					deletes++
					if !failed && !strings.HasSuffix(r.URL.Path, "/messages/resolved") {
						t.Fatalf("delete used stale identity: %s", r.URL.Path)
					}
					body, status = "", http.StatusNoContent
				default:
					t.Fatalf("unexpected request: %s", r.Method)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: h, Request: r}, nil
			})
			ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
			client := &msgraph.Client{}
			if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
				t.Fatal(err)
			}
			account := uuid.New()
			w, bus := filingWorker(account, &filingIMAP{})
			store := &warmupDeleteStore{}
			w.mailManager.Emails[account] = &wmail.WMail{ID: account, Storage: store, GraphData: &wmail.GraphData{Client: client}}
			action := models.WarmupEmailAction{EmailID: account, GmailID: "old-id", InternalID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionDelete}}
			ctx = context.WithValue(ctx, deliveryKey{}, delivery{attempt: 1, redelivers: true})
			err := w.HandleWarmupAction(ctx, action)
			if err == nil || deletes != 0 || store.deletes != 0 || len(bus.events) != 0 {
				t.Fatalf("unconfirmed lookup acknowledged or deleted: err=%v provider=%d bodies=%d events=%d", err, deletes, store.deletes, len(bus.events))
			}
			if tc.status == 503 || tc.status == 429 {
				var mailErr *errx.MailError
				if !errors.As(err, &mailErr) || mailErr.RetryAfter != 10*time.Minute || mailErr.Failure == nil || mailErr.Failure.Status != tc.status {
					t.Fatalf("provider evidence lost: %v", err)
				}
			}
			failed = false
			if err := w.HandleWarmupAction(ctx, action); err != nil || deletes != 1 || store.deletes != 1 {
				t.Fatalf("confirmed retry did not clean up: err=%v provider=%d bodies=%d", err, deletes, store.deletes)
			}
		})
	}
}

func TestGraphWarmupDeletePreservesAbsentAndLegacyCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, stableID, providerID string
		deletes                    int
	}{
		{"confirmed absent with old provider key", "<absent@example.test>", "old-id", 1},
		{"confirmed absent without provider key", "<absent@example.test>", "", 0},
		{"legacy provider key", "", "old-id", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, deletes := 0, 0
			transport := filingTransport(func(r *http.Request) (*http.Response, error) {
				body, status := `{"value":[]}`, http.StatusOK
				if r.Method == http.MethodGet {
					lookups++
				} else if r.Method == http.MethodDelete {
					deletes++
					body, status = `{"error":{"code":"ErrorItemNotFound"}}`, http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
			client := &msgraph.Client{}
			if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
				t.Fatal(err)
			}
			account := uuid.New()
			w, _ := filingWorker(account, &filingIMAP{})
			store := &warmupDeleteStore{err: errors.New("body store unavailable")}
			w.mailManager.Emails[account] = &wmail.WMail{ID: account, Storage: store, GraphData: &wmail.GraphData{Client: client}}
			action := models.WarmupEmailAction{EmailID: account, GmailID: tc.providerID, InternalID: uuid.NewString(), RFCMessageID: tc.stableID, Actions: []string{models.WarmupActionDelete}}
			ctx = context.WithValue(ctx, deliveryKey{}, delivery{attempt: 1, redelivers: true})
			if err := w.HandleWarmupAction(ctx, action); !errors.Is(err, store.err) || store.deletes != 1 {
				t.Fatalf("body-store error must remain retryable: err=%v bodies=%d", err, store.deletes)
			}
			store.err = nil
			if err := w.HandleWarmupAction(ctx, action); err != nil || deletes != 2*tc.deletes || store.deletes != 2 {
				t.Fatalf("absent/legacy cleanup changed: err=%v provider=%d bodies=%d", err, deletes, store.deletes)
			}
			if (lookups == 0) != (tc.stableID == "") {
				t.Fatalf("legacy lookup behavior changed: %d", lookups)
			}
		})
	}
}

type filingFloorAuthority struct {
	repository.SyncContextRepository
	floor     *time.Time
	lost      bool
	invalid   bool
	deferrals int
	deferErr  error
	requests  []models.WarmupFilingDeferralRequest
}

func (*filingFloorAuthority) PermittedWarmupActions(_ context.Context, _, _ uuid.UUID, actions []string) ([]string, error) {
	return actions, nil
}

func (s *filingFloorAuthority) AdmitWarmupAction(_ context.Context, req models.WarmupActionRequest) (models.WarmupActionDecision, error) {
	return models.WarmupActionDecision{Actions: req.Actions, FilingPending: true, FilingRecovery: &models.WarmupFilingRecoveryProof{
		Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: req.MailboxID, WorkerID: req.WorkerID,
		FilingID: uuid.MustParse(req.FilingID), ProviderRetryAt: s.floor,
	}}, nil
}

func (s *filingFloorAuthority) DeferWarmupFiling(_ context.Context, req models.WarmupFilingDeferralRequest) (models.WarmupFilingDeferralDecision, error) {
	s.deferrals++
	s.requests = append(s.requests, req)
	if s.deferErr != nil {
		return models.WarmupFilingDeferralDecision{}, s.deferErr
	}
	s.floor = &req.ProviderRetryAt
	if s.lost {
		return models.WarmupFilingDeferralDecision{}, errors.New("successful deferral response lost")
	}
	if s.invalid {
		return models.WarmupFilingDeferralDecision{Persisted: true, FilingRecovery: &models.WarmupFilingRecoveryProof{
			Protocol: 2, MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: req.FilingID, ProviderRetryAt: s.floor,
		}}, nil
	}
	return models.WarmupFilingDeferralDecision{Persisted: true, FilingRecovery: &models.WarmupFilingRecoveryProof{
		Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: req.FilingID, ProviderRetryAt: s.floor,
	}}, nil
}

func TestGraphFilingProviderFloorSurvivesReoffersAndLostDeferralResponse(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		for _, stage := range []string{"locate", "move"} {
			for _, responseLost := range []bool{false, true} {
				t.Run(http.StatusText(status)+"/"+stage+"/lost="+map[bool]string{false: "no", true: "yes"}[responseLost], func(t *testing.T) {
					failing, providerCalls := true, 0
					transport := filingTransport(func(r *http.Request) (*http.Response, error) {
						body, code := `{}`, http.StatusOK
						h := make(http.Header)
						switch {
						case strings.HasSuffix(r.URL.Path, "/messages"):
							body = `{"value":[{"id":"resolved"}]}`
						case strings.HasSuffix(r.URL.Path, "/mailFolders/deleteditems"):
							body = `{"id":"trash-id"}`
						case strings.HasSuffix(r.URL.Path, "/mailFolders"):
							body = `{"value":[{"id":"target-id","displayName":"Warmbly"}]}`
						case strings.HasSuffix(r.URL.Path, "/messages/resolved"):
							body = `{"parentFolderId":"inbox-id"}`
						case strings.HasSuffix(r.URL.Path, "/messages/resolved/move"):
							body = `{"id":"moved"}`
						default:
							t.Fatalf("unexpected Graph request %s", r.URL.Path)
						}
						if stage == "locate" && strings.HasSuffix(r.URL.Path, "/messages") || stage == "move" && strings.HasSuffix(r.URL.Path, "/move") {
							providerCalls++
							if failing {
								code, body = status, `{"error":{"code":"Unavailable","message":"synthetic retry floor"}}`
								h.Set("Retry-After", "600")
							}
						}
						return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: h, Request: r}, nil
					})
					ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
					client := &msgraph.Client{}
					if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
						t.Fatal(err)
					}
					account, worker := uuid.New(), uuid.New()
					w, bus := filingWorker(account, &filingIMAP{})
					w.ID = worker.String()
					w.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
					authority := &filingFloorAuthority{lost: responseLost}
					w.SyncContextRepository = authority
					action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionFile}}
					before := time.Now()
					err := w.HandleWarmupAction(ctx, action)
					if (err != nil) != responseLost || providerCalls != 1 || len(bus.events) != 0 || authority.deferrals != 1 || authority.floor == nil || authority.floor.Before(before.Add(10*time.Minute)) {
						t.Fatalf("unconfirmed floor acknowledgment or lost native retry deadline: err=%v calls=%d deferrals=%d floor=%v events=%d", err, providerCalls, authority.deferrals, authority.floor, len(bus.events))
					}
					// A different process can read only the persisted floor on admission.
					newWorker, restartedBus := filingWorker(account, &filingIMAP{})
					newWorker.ID, newWorker.SyncContextRepository = w.ID, authority
					newWorker.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
					if err := newWorker.HandleWarmupAction(ctx, action); err != nil || providerCalls != 1 || len(restartedBus.events) != 0 || authority.deferrals != 1 {
						t.Fatalf("restarted reoffer bypassed floor: err=%v calls=%d events=%d", err, providerCalls, len(restartedBus.events))
					}
					authority.lost = false
					failing = false
					past := time.Now().Add(-time.Second)
					authority.floor = &past
					if err := newWorker.HandleWarmupAction(ctx, action); err != nil || providerCalls != 2 || len(restartedBus.events) != 1 || restartedBus.events[0].Type != models.JobEventTypeWarmupFiled {
						t.Fatalf("expired floor did not resume filing: err=%v calls=%d events=%+v", err, providerCalls, restartedBus.events)
					}
				})
			}
		}
	}
}

func TestGraphFilingRejectsUnconfirmedDeferralProof(t *testing.T) {
	authority := &filingFloorAuthority{invalid: true}
	w, bus := filingWorker(uuid.New(), &filingIMAP{})
	w.ID = uuid.NewString()
	w.SyncContextRepository = authority
	w.mailManager.Emails = map[uuid.UUID]*wmail.WMail{}
	account := uuid.New()
	client := &msgraph.Client{}
	transport := filingTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"600"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"Unavailable"}}`)), Request: r}, nil
	})
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
	if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
		t.Fatal(err)
	}
	w.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
	action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionFile}}
	if err := w.HandleWarmupAction(ctx, action); err == nil || authority.deferrals != 1 || len(bus.events) != 0 {
		t.Fatalf("unsupported deferral claimed durable: err=%v calls=%d events=%v", err, authority.deferrals, bus.events)
	}
}

func TestWarmupProviderRetryAtUsesOnlyNativeAbsoluteEvidence(t *testing.T) {
	observed := time.Date(2026, 10, 11, 14, 0, 0, 123, time.FixedZone("UTC+02", 2*60*60))
	absolute := observed.Add(40 * time.Minute)
	failure := *errx.ErrMailServerUnreachable
	failure.RetryAfter = time.Minute
	failure.Failure = &errx.SendFailure{ObservedAt: observed, RetryAt: &absolute}
	if got := warmupProviderRetryAt(&failure); !got.Equal(absolute) || got.Location() != time.UTC {
		t.Fatalf("relative guidance shortened absolute UTC floor: %s", got)
	}
	failure.RetryAfter = time.Hour
	if got := warmupProviderRetryAt(&failure); !got.Equal(absolute) {
		t.Fatalf("larger relative guidance replaced native deadline: %s", got)
	}
	for _, f := range []*errx.SendFailure{nil, {ObservedAt: observed}} {
		failure.Failure = f
		if got := warmupProviderRetryAt(&failure); !got.IsZero() {
			t.Fatalf("relative-only guidance invented persisted floor: %s", got)
		}
	}
	if got := warmupProviderRetryAt(errors.New("unclassified provider failure")); !got.IsZero() {
		t.Fatalf("unknown error invented provider floor: %s", got)
	}
}

func TestWarmupFilingRelativeRetryAfterDoesNotPersistFloor(t *testing.T) {
	for _, observed := range []time.Time{{}, time.Now().UTC()} {
		account := uuid.New()
		failure := *errx.ErrMailServerUnreachable
		failure.RetryAfter = 10 * time.Minute
		if !observed.IsZero() {
			failure.Failure = &errx.SendFailure{ObservedAt: observed}
		}
		w, bus := filingWorker(account, &filingIMAP{found: map[string]uint32{"INBOX": 11}, err: &failure})
		w.ID = uuid.NewString()
		authority := &filingFloorAuthority{}
		w.SyncContextRepository = authority
		action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<relative@example.test>", Actions: []string{models.WarmupActionFile}}
		if err := w.HandleWarmupAction(t.Context(), action); err != nil || authority.deferrals != 0 || authority.floor != nil || len(bus.events) != 0 {
			t.Fatalf("relative guidance became durable floor: err=%v deferrals=%d floor=%v events=%v", err, authority.deferrals, authority.floor, bus.events)
		}
		if _, found := w.pendingFilingDeferrals.Load(uuid.MustParse(action.FilingID)); found {
			t.Fatal("relative-only guidance retained a pending floor")
		}
	}
}

func TestGraphFilingLegacyRetryPolicyIsUnchangedWithProviderFloor(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, attempt := range []int{warmupDeleteRedeliveries - 1, warmupDeleteRedeliveries} {
			transport := filingTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"600"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"Unavailable"}}`)), Request: r}, nil
			})
			ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
			client := &msgraph.Client{}
			if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
				t.Fatal(err)
			}
			account := uuid.New()
			w, bus := filingWorker(account, &filingIMAP{})
			w.ID, w.SyncContextRepository = uuid.NewString(), &warmupFilingAuthorityStub{pending: pending}
			w.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
			ctx = context.WithValue(ctx, deliveryKey{}, delivery{attempt: attempt, redelivers: true})
			err := w.HandleWarmupAction(ctx, models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<legacy@example.test>", Actions: []string{models.WarmupActionFile}})
			if (err != nil) != (!pending && attempt < warmupDeleteRedeliveries) || len(bus.events) != 0 {
				t.Fatalf("legacy policy changed: pending=%v attempt=%d err=%v events=%v", pending, attempt, err, bus.events)
			}
		}
	}
}

func TestGraphFilingRetriesUnconfirmedPersistenceWithoutProviderReoffer(t *testing.T) {
	providerCalls := 0
	transport := filingTransport(func(r *http.Request) (*http.Response, error) {
		providerCalls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"600"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"Unavailable"}}`)), Request: r}, nil
	})
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
	client := &msgraph.Client{}
	if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
		t.Fatal(err)
	}
	account := uuid.New()
	w, bus := filingWorker(account, &filingIMAP{})
	w.ID = uuid.NewString()
	w.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
	backendFailure := errors.New("floor store unavailable")
	authority := &filingFloorAuthority{deferErr: backendFailure}
	w.SyncContextRepository = authority
	action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionFile}}
	for range 2 {
		if err := w.HandleWarmupAction(ctx, action); !errors.Is(err, backendFailure) {
			t.Fatalf("failed persistence acknowledged: %v", err)
		}
	}
	if providerCalls != 1 || authority.deferrals != 2 || len(bus.events) != 0 {
		t.Fatalf("unconfirmed persistence retried provider early: provider=%d deferrals=%d events=%v", providerCalls, authority.deferrals, bus.events)
	}
	authority.deferErr = nil
	if err := w.HandleWarmupAction(ctx, action); err != nil || providerCalls != 1 || authority.deferrals != 3 || len(bus.events) != 0 || !authority.requests[0].ProviderRetryAt.Equal(authority.requests[2].ProviderRetryAt) {
		t.Fatalf("retry did not retain original error deadline: err=%v provider=%d deferrals=%d", err, providerCalls, authority.deferrals)
	}
}

func TestDurableGmailFilingResolvesStableIDAndRetriesProviderFailure(t *testing.T) {
	failing, modifications := true, 0
	transport := filingTransport(func(r *http.Request) (*http.Response, error) {
		body, status := `{}`, http.StatusOK
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			body = `{"messages":[{"id":"resolved"}]}`
		case strings.HasSuffix(r.URL.Path, "/labels"):
			body = `{"labels":[{"id":"label","name":"Warmbly"}]}`
		case strings.HasSuffix(r.URL.Path, "/messages/resolved"):
			body = `{"id":"resolved","labelIds":["INBOX"]}`
		case strings.HasSuffix(r.URL.Path, "/messages/resolved/modify"):
			modifications++
			if failing {
				status, body = http.StatusServiceUnavailable, `{"error":{"code":503,"message":"provider unavailable"}}`
			}
		default:
			t.Errorf("filing used a stale provider ID or unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: transport})
	client := &goog.Client{}
	if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
		t.Fatal(err)
	}
	w := newLoadingWorker(&capturedEvents{})
	account := uuid.New()
	w.mailManager.Emails[account] = &wmail.WMail{GoogleData: &wmail.GoogleData{Client: client}}
	bus := &filingBus{}
	w.Bus, w.Codec = bus, codec.NewJSON()
	action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), GmailID: "stale",
		RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionFile}}
	if err := w.HandleWarmupAction(ctx, action); err == nil || len(bus.events) != 0 {
		t.Fatal("Gmail failure was acknowledged or discarded")
	}
	failing = false
	if err := w.HandleWarmupAction(ctx, action); err != nil {
		t.Fatal(err)
	}
	if modifications != 2 || len(bus.events) != 1 || bus.events[0].Type != models.JobEventTypeWarmupFiled {
		t.Fatalf("Gmail filing did not retry and acknowledge: modifications=%d events=%+v", modifications, bus.events)
	}
}

func TestDurableGraphFilingRetriesWithoutRestoringTrashedMail(t *testing.T) {
	failing, moves, parent := true, 0, "inbox-id"
	transport := filingTransport(func(r *http.Request) (*http.Response, error) {
		body, status := `{}`, http.StatusOK
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			body = `{"value":[{"id":"resolved"}]}`
		case strings.HasSuffix(r.URL.Path, "/mailFolders/deleteditems"):
			body = `{"id":"trash-id"}`
		case strings.HasSuffix(r.URL.Path, "/mailFolders"):
			body = `{"value":[{"id":"target-id","displayName":"Warmbly"}]}`
		case strings.HasSuffix(r.URL.Path, "/messages/resolved"):
			body = `{"parentFolderId":"` + parent + `"}`
		case strings.HasSuffix(r.URL.Path, "/messages/resolved/move"):
			if failing {
				status, body = http.StatusServiceUnavailable, `{"error":{"code":"Unavailable","message":"provider unavailable"}}`
			} else {
				moves++
				parent = "target-id"
				body = `{"id":"resolved"}`
			}
		default:
			t.Errorf("unexpected Graph filing request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: transport})
	client := &msgraph.Client{}
	if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
		t.Fatal(err)
	}
	w := newLoadingWorker(&capturedEvents{})
	account := uuid.New()
	w.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
	bus := &filingBus{}
	w.Bus, w.Codec = bus, codec.NewJSON()
	action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionFile}}
	if err := w.HandleWarmupAction(ctx, action); err == nil || len(bus.events) != 0 {
		t.Fatal("Graph failure was acknowledged or discarded")
	}
	failing = false
	for range 2 {
		if err := w.HandleWarmupAction(ctx, action); err != nil {
			t.Fatal(err)
		}
	}
	if moves != 1 || len(bus.events) != 2 {
		t.Fatalf("Graph filing did not retry idempotently: moves=%d events=%+v", moves, bus.events)
	}
	parent = "trash-id"
	if err := w.HandleWarmupAction(ctx, action); err != nil {
		t.Fatal(err)
	}
	if moves != 1 || len(bus.events) != 3 {
		t.Fatal("Graph filing restored a message from Trash or failed to resolve the pending filing")
	}
}

func TestDurableGraphFilingFailureReleasesCommandWithoutFilingAcknowledgement(t *testing.T) {
	for _, stage := range []string{"locate", "move"} {
		t.Run(stage, func(t *testing.T) {
			failing := true
			transport := filingTransport(func(r *http.Request) (*http.Response, error) {
				body, status := `{}`, http.StatusOK
				switch {
				case strings.HasSuffix(r.URL.Path, "/messages"):
					body = `{"value":[{"id":"resolved"}]}`
					if failing && stage == "locate" {
						status = http.StatusServiceUnavailable
					}
				case strings.HasSuffix(r.URL.Path, "/mailFolders/deleteditems"):
					body = `{"id":"trash-id"}`
				case strings.HasSuffix(r.URL.Path, "/mailFolders"):
					body = `{"value":[{"id":"target-id","displayName":"Warmbly"}]}`
				case strings.HasSuffix(r.URL.Path, "/messages/resolved"):
					body = `{"parentFolderId":"inbox-id"}`
				case strings.HasSuffix(r.URL.Path, "/messages/resolved/move"):
					if failing {
						status = http.StatusServiceUnavailable
					}
					body = `{"id":"resolved"}`
				default:
					t.Errorf("unexpected Graph request: %s", r.URL.Path)
				}
				if status == http.StatusServiceUnavailable {
					body = `{"error":{"code":"Unavailable","message":"provider unavailable"}}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: transport})
			client := &msgraph.Client{}
			if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
				t.Fatal(err)
			}
			account := uuid.New()
			w, bus := filingWorker(account, &filingIMAP{})
			w.ID = uuid.NewString()
			w.mailManager.Emails[account] = &wmail.WMail{GraphData: &wmail.GraphData{Client: client}}
			authority := &warmupFilingAuthorityStub{pending: true}
			w.SyncContextRepository = authority
			action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<warmup@example.test>", Actions: []string{models.WarmupActionFile}}
			ctx = context.WithValue(ctx, deliveryKey{}, delivery{attempt: 1, redelivers: true})
			if err := w.HandleWarmupAction(ctx, action); err != nil || len(bus.events) != 0 {
				t.Fatalf("durable Graph failure blocked the command or falsely acknowledged filing: %v %+v", err, bus.events)
			}
			// A healthy mailbox can run before the control plane retries the failed filing.
			other := uuid.New()
			w.mailManager.Emails[other] = &wmail.WMail{SmtpImapData: &wmail.SmtpImapData{ImapClient: &filingIMAP{found: map[string]uint32{"INBOX": 12}}, Mailboxes: []*models.Mailbox{{Name: "INBOX", UIDValidity: 77}}}}
			if err := w.HandleWarmupAction(ctx, models.WarmupEmailAction{EmailID: other, FilingID: uuid.NewString(), RFCMessageID: "<other@example.test>", Actions: []string{models.WarmupActionFile}}); err != nil || len(bus.events) != 1 {
				t.Fatalf("healthy filing blocked: %v", err)
			}
			failing = false
			if err := w.HandleWarmupAction(ctx, action); err != nil || len(bus.events) != 2 {
				t.Fatalf("durable retry did not acknowledge success: %v %+v", err, bus.events)
			}
			bus.err = errors.New("result broker unavailable")
			if err := w.HandleWarmupAction(ctx, action); err == nil {
				t.Fatal("filing acknowledgement failure was discarded")
			}
		})
	}
}
