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

func TestRunRetriesRequestedReloadAndKeepsRequestsMadeDuringHeartbeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var a *Agent
		var beats []models.NodeHeartbeat
		client := &http.Client{Transport: heartbeatTransport(func(r *http.Request) (*http.Response, error) {
			var beat models.NodeHeartbeat
			if err := json.NewDecoder(r.Body).Decode(&beat); err != nil {
				return nil, err
			}
			beats = append(beats, beat)
			if len(beats) == 3 {
				return nil, errors.New("lost reload acknowledgement")
			}
			if len(beats) == 4 {
				a.RequestReload()
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"liveness_seconds":120}`)), Header: make(http.Header)}, nil
		})}
		a = New(Config{NodeID: uuid.New(), Role: models.NodeRoleWorker, BaseURL: "https://backend.test", Token: "fixture", Address: "1.1.1.1", HTTPClient: client})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go a.Run(ctx)
		synctest.Wait()
		for i := 1; i < 6; i++ {
			if i == 2 {
				a.RequestReload()
				a.RequestReload()
			}
			time.Sleep(40 * time.Second)
			synctest.Wait()
		}
		for i, want := range []bool{true, false, true, true, true, false} {
			if len(beats) <= i || beats[i].Booted != want {
				t.Fatalf("heartbeat %d: expected reload=%t, beats=%+v", i+1, want, beats)
			}
		}
		cancel()
		synctest.Wait()
	})
}
