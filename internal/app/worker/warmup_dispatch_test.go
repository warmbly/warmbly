package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

type dispatchAuthority struct {
	repository.SyncContextRepository
	mu     sync.Mutex
	nonce  uuid.UUID
	state  string
	result *models.SendEmailResult
}

func (a *dispatchAuthority) WarmupDispatch(_ context.Context, _, _, _, nonce uuid.UUID, start bool, result *models.SendEmailResult) (*repository.WarmupDispatchState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if result != nil {
		copy := *result
		a.result = &copy
		a.state = "finished"
		return nil, nil
	}
	if start {
		if nonce != a.nonce {
			return nil, errors.New("foreign nonce")
		}
		if a.state == "authorized" {
			a.state = "started"
			return &repository.WarmupDispatchState{State: "execute"}, nil
		}
	}
	return &repository.WarmupDispatchState{State: a.state, Result: a.result}, nil
}

type dispatchBodyStore struct {
	storage.Store
	data []byte
}

func (s dispatchBodyStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

type dispatchResultBus struct {
	eventbus.EventBus
	mu     sync.Mutex
	fail   bool
	events int
}

func (b *dispatchResultBus) Publish(context.Context, string, string, []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail {
		b.fail = false
		return errors.New("lost broker acknowledgement")
	}
	b.events++
	return nil
}

type dispatchNativeTransport struct{ calls atomic.Int32 }

func (r *dispatchNativeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":429,"message":"fixture"}}`)), Request: req}, nil
}

func TestWarmupWorkerExecutesNativeOnceAndReplaysAfterLostEventAndRestart(t *testing.T) {
	rt := &dispatchNativeTransport{}
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: rt})
	client := &goog.Client{Email: "sender@example.test"}
	if err := client.InitWithSource(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "fixture-token"})); err != nil {
		t.Fatal(err)
	}
	authority := &dispatchAuthority{nonce: uuid.New(), state: "authorized"}
	bus := &dispatchResultBus{fail: true}
	worker := newLoadingWorker(&capturedEvents{})
	worker.ID = uuid.NewString()
	worker.SyncContextRepository = authority
	worker.Bus = bus
	worker.Codec = codec.NewJSON()
	id := uuid.New()
	worker.mailManager.Emails[id] = &wmail.WMail{EmailType: models.InboxProviderGoogle, GoogleData: &wmail.GoogleData{Client: client}}
	blob := &emsg.EmailBlob{PlainText: []byte("Simulated diagnostic."), DispatchNonce: authority.nonce.String()}
	data, err := blob.EncodeBinary()
	if err != nil {
		t.Fatal(err)
	}
	worker.Storage = dispatchBodyStore{data: data}
	command := models.SendEmail{TaskID: uuid.New(), EmailID: id, To: []string{"recipient@example.test"}, Subject: "Simulated diagnostic", BodyS3Key: "fixture", IsWarmup: true}
	if err := worker.HandleSendEmail(ctx, command); err == nil {
		t.Fatal("broker failure was acknowledged")
	}
	if rt.calls.Load() != 1 || authority.state != "finished" {
		t.Fatal("native outcome was not saved before publication")
	}
	restarted := &WorkerService{ID: worker.ID, SyncContextRepository: authority, Bus: bus, Codec: codec.NewJSON()}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := restarted.HandleSendEmail(ctx, command); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if rt.calls.Load() != 1 || bus.events != 12 {
		t.Fatal("redelivery re-executed native send or lost durable result")
	}
	authority.state = "authorized"
	blob.DispatchNonce = ""
	data, err = blob.EncodeBinary()
	if err != nil {
		t.Fatal(err)
	}
	worker.Storage = dispatchBodyStore{data: data}
	if err := worker.HandleSendEmail(ctx, command); err == nil {
		t.Fatal("nonce-gated dispatch accepted legacy body")
	}
	blob.DispatchNonce = uuid.NewString()
	data, err = blob.EncodeBinary()
	if err != nil {
		t.Fatal(err)
	}
	worker.Storage = dispatchBodyStore{data: data}
	if err := worker.HandleSendEmail(ctx, command); err == nil {
		t.Fatal("foreign nonce executed")
	}
	worker.ID = "invalid"
	if err := worker.HandleSendEmail(ctx, command); err == nil {
		t.Fatal("malformed worker identity executed")
	}
	if rt.calls.Load() != 1 {
		t.Fatal("rejected dispatch reached native transport")
	}
}
