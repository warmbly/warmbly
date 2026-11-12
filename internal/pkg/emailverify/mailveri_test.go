package emailverify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMailVeriMapsResults(t *testing.T) {
	busyCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			http.Error(w, "API 403: Forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v1/get-balance/":
			_, _ = w.Write([]byte(`{"error":0,"message":"You have 42 credits","balance":42}`))
		case "/v1/verify-mail/":
			var in struct {
				Email string `json:"email"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			switch in.Email {
			case "good@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"VALID","email":"good@x.com","balance":41,"credits_used":1,"quality_score":96,"is_forwarding":false}`))
			case "info@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"ROLE_ACCOUNT","email":"info@x.com","balance":40,"credits_used":1,"quality_score":70,"is_forwarding":false}`))
			case "any@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"CATCH_ALL","email":"any@x.com","balance":39,"credits_used":1,"quality_score":55,"is_forwarding":false}`))
			case "temp@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"DISPOSABLE","email":"temp@x.com","balance":38,"credits_used":1,"quality_score":5,"is_forwarding":false}`))
			case "full@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"FULL_INBOX","email":"full@x.com","balance":37,"credits_used":1,"quality_score":30,"is_forwarding":false}`))
			case "gone@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"INVALID","email":"gone@x.com","balance":36,"credits_used":1,"quality_score":0,"is_forwarding":false}`))
			case "slow@x.com":
				_, _ = w.Write([]byte(`{"error":0,"status":"UNKNOWN","email":"slow@x.com","balance":36,"credits_used":0,"quality_score":0,"is_forwarding":false}`))
			case "busy@x.com":
				busyCalls++
				w.WriteHeader(http.StatusTooManyRequests)
			default:
				_, _ = w.Write([]byte(`{"error":2,"message":"Not enough credits"}`))
			}
		}
	}))
	defer srv.Close()

	mv := NewMailVeri("key", srv.URL)
	if n, err := mv.Account(context.Background()); err != nil || n == nil || *n != 42 {
		t.Fatalf("account = %v, %v", n, err)
	}
	if !mv.ObservesBalance() {
		t.Fatal("mailveri reports its balance")
	}
	res, err := mv.Check(context.Background(), "good@x.com")
	if err != nil || res.Status != StatusValid || res.Provider != ProviderMailVeri || res.Confidence != 96 || !res.HasMX {
		t.Fatalf("good = %+v, %v", res, err)
	}
	cases := []struct {
		email string
		want  Status
		sub   SubStatus
	}{
		{"info@x.com", StatusRisky, SubStatusRole},
		{"any@x.com", StatusRisky, SubStatusCatchAll},
		{"temp@x.com", StatusInvalid, SubStatusDisposable},
		{"full@x.com", StatusRisky, SubStatusMailboxFull},
		{"gone@x.com", StatusInvalid, ""},
		{"slow@x.com", StatusUnknown, ""},
	}
	for _, c := range cases {
		res, err := mv.Check(context.Background(), c.email)
		if err != nil || res.Status != c.want || res.SubStatus != c.sub {
			t.Fatalf("%s = %+v, %v", c.email, res, err)
		}
	}
	if res, _ := mv.Check(context.Background(), "any@x.com"); !res.IsCatchAll {
		t.Fatalf("catch-all flag = %+v", res)
	}
	if res, _ := mv.Check(context.Background(), "gone@x.com"); res.HasMX || res.Reason != "mailveri: invalid" {
		t.Fatalf("gone = %+v", res)
	}
	res, err = mv.Check(context.Background(), "broke@x.com")
	if !errors.Is(err, ErrProviderCredits) || res.Status != StatusUnknown {
		t.Fatalf("no credits = %+v, %v", res, err)
	}
	res, err = mv.Check(context.Background(), "busy@x.com")
	if err == nil || errors.Is(err, ErrProviderKey) || res.Status != StatusUnknown {
		t.Fatalf("rate limited = %+v, %v", res, err)
	}
	if busyCalls != mailVeriRateLimitAttempts {
		t.Fatalf("rate limited request tried %d times, want %d", busyCalls, mailVeriRateLimitAttempts)
	}

	bad := NewMailVeri("wrong", srv.URL)
	if _, err := bad.Account(context.Background()); !errors.Is(err, ErrProviderKey) {
		t.Fatalf("bad key account err = %v", err)
	}
	if res, err := bad.Check(context.Background(), "good@x.com"); !errors.Is(err, ErrProviderKey) || res.Status != StatusUnknown {
		t.Fatalf("bad key check = %+v, %v", res, err)
	}
}

func TestMailVeriRetriesARateLimitedCheck(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"error":0,"status":"VALID","email":"good@x.com","balance":9,"credits_used":1,"quality_score":90,"is_forwarding":false}`))
	}))
	defer srv.Close()

	res, err := NewMailVeri("key", srv.URL).Check(context.Background(), "good@x.com")
	if err != nil || res.Status != StatusValid || calls != 2 {
		t.Fatalf("retry = %+v, %v after %d calls", res, err, calls)
	}
}

func TestMailVeriCapsChecksInFlight(t *testing.T) {
	var (
		mu       sync.Mutex
		active   int
		maxSeen  int
		release  = make(chan struct{})
		arrivals = make(chan struct{}, 16)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > maxSeen {
			maxSeen = active
		}
		mu.Unlock()
		arrivals <- struct{}{}
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		_, _ = w.Write([]byte(`{"error":0,"status":"VALID","email":"a@x.com","balance":1,"credits_used":1,"quality_score":90,"is_forwarding":false}`))
	}))
	defer srv.Close()

	// Separate clients for the same key share the limit.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = NewMailVeri("cap-key", srv.URL).Check(context.Background(), "a@x.com")
		}()
	}
	for i := 0; i < mailVeriMaxInFlight; i++ {
		<-arrivals
	}
	select {
	case <-arrivals:
		t.Fatal("a sixth check started while five were in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if maxSeen != mailVeriMaxInFlight {
		t.Fatalf("max in flight = %d, want %d", maxSeen, mailVeriMaxInFlight)
	}
}
