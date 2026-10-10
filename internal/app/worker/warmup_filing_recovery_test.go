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
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
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
