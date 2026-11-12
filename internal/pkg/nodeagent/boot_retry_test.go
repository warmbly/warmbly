package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type heartbeatTransport func(*http.Request) (*http.Response, error)

func (f heartbeatTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRunRetriesBootUntilAcknowledged(t *testing.T) {
	for _, role := range []models.NodeRole{models.NodeRoleWorker, models.NodeRoleConsumer} {
		t.Run(string(role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				received := make(chan models.NodeHeartbeat, 8)
				calls := 0
				client := &http.Client{Transport: heartbeatTransport(func(r *http.Request) (*http.Response, error) {
					var beat models.NodeHeartbeat
					if err := json.NewDecoder(r.Body).Decode(&beat); err != nil {
						return nil, err
					}
					received <- beat
					calls++
					status, body := http.StatusOK, `{"liveness_seconds":120}`
					switch calls {
					case 1:
						return nil, errors.New("temporary network failure")
					case 2:
						status = http.StatusServiceUnavailable
					case 3:
						body = "incomplete reply"
					case 4:
						body = "null"
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})}
				a := New(Config{NodeID: uuid.New(), Role: role, BaseURL: "https://backend.test", Token: "fixture", Address: "1.1.1.1", HTTPClient: client})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan struct{})
				go func() { a.Run(ctx); close(done) }()
				for i := range 6 {
					if i > 0 {
						interval := DefaultInterval
						if i == 5 {
							interval = 40 * time.Second
						}
						time.Sleep(interval)
					}
					synctest.Wait()
					select {
					case beat := <-received:
						if beat.Booted != (i < 5) || beat.Stopping {
							t.Fatalf("heartbeat %d: Booted=%t Stopping=%t", i+1, beat.Booted, beat.Stopping)
						}
					default:
						t.Fatalf("heartbeat %d was not sent", i+1)
					}
				}
				cancel()
				synctest.Wait()
				<-done
				if beat := <-received; !beat.Stopping || beat.Booted {
					t.Fatalf("farewell repeated boot or did not stop: %+v", beat)
				}
			})
		})
	}
}

// An old backend only knows the boot flag, so a miss asks for a full reload; once a reply says the backend
// reloads by id, a miss names its mailbox, and a beat that is lost sends the same names again.
func TestRunNamesMissingMailboxesOnceTheBackendReloadsByID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var a *Agent
		first, second := uuid.New(), uuid.New()
		var beats []models.NodeHeartbeat
		client := &http.Client{Transport: heartbeatTransport(func(r *http.Request) (*http.Response, error) {
			var beat models.NodeHeartbeat
			if err := json.NewDecoder(r.Body).Decode(&beat); err != nil {
				return nil, err
			}
			beats = append(beats, beat)
			body := `{"liveness_seconds":120}`
			switch len(beats) {
			case 2, 3:
				body = `{"liveness_seconds":120,"reloads_by_id":true}`
			case 4:
				a.RequestReload(second)
				return nil, errors.New("lost reload acknowledgement")
			}
			if len(beats) >= 5 {
				body = `{"liveness_seconds":120,"reloads_by_id":true}`
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
		a = New(Config{NodeID: uuid.New(), Role: models.NodeRoleWorker, BaseURL: "https://backend.test", Token: "fixture", Address: "1.1.1.1", HTTPClient: client})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go a.Run(ctx)
		synctest.Wait()
		a.RequestReload(first)
		for range 5 {
			time.Sleep(40 * time.Second)
			synctest.Wait()
			if len(beats) == 2 {
				a.RequestReload(first)
			}
		}
		if len(beats) < 6 {
			t.Fatalf("got %d beats, want 6", len(beats))
		}
		if !beats[0].Booted || len(beats[0].MissingMailboxes) != 0 {
			t.Errorf("beat 1 = %+v, want the boot flag alone", beats[0])
		}
		if !beats[1].Booted || len(beats[1].MissingMailboxes) != 0 {
			t.Errorf("beat 2 = %+v, want a miss before reloads_by_id to ask for a full reload", beats[1])
		}
		if beats[2].Booted || len(beats[2].MissingMailboxes) != 1 || beats[2].MissingMailboxes[0] != first {
			t.Errorf("beat 3 = %+v, want only the missing mailbox named", beats[2])
		}
		if beats[3].Booted || len(beats[3].MissingMailboxes) != 0 {
			t.Errorf("beat 4 = %+v, want nothing pending", beats[3])
		}
		if beats[4].Booted || len(beats[4].MissingMailboxes) != 1 || beats[4].MissingMailboxes[0] != second {
			t.Errorf("beat 5 = %+v, want the request made during the lost beat", beats[4])
		}
		if len(beats[5].MissingMailboxes) != 0 {
			t.Errorf("beat 6 = %+v, want nothing pending once acknowledged", beats[5])
		}
		cancel()
		synctest.Wait()
	})
}
