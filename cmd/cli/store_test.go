package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
)

// storeServer records every request and answers each with the handler's reply.
type storeServer struct {
	mu   sync.Mutex
	reqs []capturedRequest
	keys []string
}

func (s *storeServer) start(t *testing.T, reply func(n int, r *http.Request, body []byte) (int, string)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, capturedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body})
		s.keys = append(s.keys, r.Header.Get("Idempotency-Key"))
		n := len(s.reqs)
		s.mu.Unlock()
		status, payload := reply(n, r, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// runCLI runs one invocation with no terminal on stdin and no --yes added.
func runCLI(t *testing.T, url string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("WARMBLY_CONFIG_DIR", t.TempDir())
	t.Setenv("WARMBLY_TOKEN", "wmbly_test")
	t.Setenv("WARMBLY_API_URL", url)
	var out bytes.Buffer
	f := &Factory{IO: &iostreams.IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &out}}
	root := newRootCmd(f)
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func quoteJSON(id string, due int64) string {
	return fmt.Sprintf(`{"id":%q,"expires_at":"2026-11-18T12:15:00Z","lines":[{"type":"domain","domain":"getacme.com","years":1,"amount_micros":12000000,"renew_micros":12000000},{"type":"mailboxes","domain":"getacme.com","provider":"google","count":2,"amount_micros":%d,"monthly_micros":6000000}],"due_now_micros":%d,"recurring_monthly_micros":6000000,"yearly_domain_renewal_micros":12000000,"payment":{"balance_micros":50000000,"from_balance_micros":%d}}`, id, due-12000000, due, due)
}

var buyArgs = []string{
	"store", "buy", "--domain", "getacme.com", "--mailbox", "ada@getacme.com", "--mailbox", "grace@getacme.com:Grace:Hopper",
	"--provider", "google", "--first-name", "Ada", "--last-name", "Lovelace", "--admin", "ada@getacme.com",
	"--forwarding-url", "https://acme.com",
}

func TestStoreBuyQuotesThenOrdersAtTheQuotedTotal(t *testing.T) {
	var s storeServer
	url := s.start(t, func(n int, r *http.Request, _ []byte) (int, string) {
		if r.URL.Path == "/v1/store/quotes" {
			return http.StatusCreated, quoteJSON("11111111-1111-1111-1111-111111111111", 18_000_000)
		}
		return http.StatusCreated, `{"id":"o1","status":"charging","steps":[]}`
	})
	if out, err := runCLI(t, url, append(buyArgs, "--yes", "--json")...); err != nil {
		t.Fatalf("buy: %v\n%s", err, out)
	}
	if len(s.reqs) != 2 || s.reqs[0].path != "/v1/store/quotes" || s.reqs[1].path != "/v1/store/orders" {
		t.Fatalf("requests: %+v", s.reqs)
	}

	var q store.QuoteRequest
	strictDecode(t, s.reqs[0].body, &q)
	if len(q.Items) != 2 || q.Items[0].Type != "domain" || q.Items[0].Domain != "getacme.com" || q.Items[1].Type != "mailboxes" || q.Items[1].Provider != "google" {
		t.Fatalf("quote items: %+v", q.Items)
	}
	mbs := q.Items[1].Mailboxes
	if len(mbs) != 2 || mbs[0].LocalPart != "ada" || mbs[0].FirstName != "Ada" || !mbs[0].Admin ||
		mbs[1].LocalPart != "grace" || mbs[1].LastName != "Hopper" || mbs[1].Admin {
		t.Fatalf("mailboxes: %+v", mbs)
	}

	var o store.OrderRequest
	strictDecode(t, s.reqs[1].body, &o)
	if o.QuoteID.String() != "11111111-1111-1111-1111-111111111111" || o.ExpectedDueNowMicros == nil || *o.ExpectedDueNowMicros != 18_000_000 || o.ForwardingURL != "https://acme.com" {
		t.Fatalf("order: %s", s.reqs[1].body)
	}
}

func TestStoreBuyRefusesWithoutATerminalOrYes(t *testing.T) {
	var s storeServer
	url := s.start(t, func(int, *http.Request, []byte) (int, string) { return http.StatusOK, `{}` })
	_, err := runCLI(t, url, buyArgs...)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("a buy with no terminal and no --yes must refuse, got %v", err)
	}
	if len(s.reqs) != 0 {
		t.Fatalf("a refused buy made %d requests", len(s.reqs))
	}
}

func priceChanged(id string, due int64) string {
	return `{"error":"Conflict","code":"price_changed","request_id":"r1","message":"The price changed","details":{"quote":` + quoteJSON(id, due) + `}}`
}

func TestStoreBuyStopsOnAPriceChangeUnderYes(t *testing.T) {
	var s storeServer
	url := s.start(t, func(n int, r *http.Request, _ []byte) (int, string) {
		if r.URL.Path == "/v1/store/quotes" {
			return http.StatusCreated, quoteJSON("q1", 18_000_000)
		}
		return http.StatusConflict, priceChanged("q2", 21_500_000)
	})
	_, err := runCLI(t, url, append(buyArgs, "--yes")...)
	if err == nil || !strings.Contains(err.Error(), "$21.50") || !strings.Contains(err.Error(), "Nothing was charged") {
		t.Fatalf("want the new total named, got %v", err)
	}
	if len(s.reqs) != 2 {
		t.Fatalf("--yes must not buy at a price nobody saw; made %d requests", len(s.reqs))
	}
}

func TestStoreBuyAsksAgainAtTheNewPrice(t *testing.T) {
	var s storeServer
	url := s.start(t, func(n int, r *http.Request, _ []byte) (int, string) {
		switch {
		case r.URL.Path == "/v1/store/quotes":
			return http.StatusCreated, quoteJSON("q1", 18_000_000)
		case n == 2:
			return http.StatusConflict, priceChanged("q2", 21_500_000)
		}
		return http.StatusCreated, `{"id":"o1","status":"charging"}`
	})
	var asked []string
	var errOut bytes.Buffer
	b := &storeBuy{
		client: api.New(url, "wmbly_test", "test"), io: &iostreams.IOStreams{Out: &errOut, ErrOut: &errOut},
		idemKey: "k", maxTries: 3,
		ask: func(q string) (bool, error) { asked = append(asked, q); return true, nil },
	}
	if _, err := b.run(context.Background(), []byte(`{"items":[]}`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(asked) != 2 || !strings.Contains(asked[1], "$21.50") {
		t.Fatalf("questions: %q", asked)
	}
	if len(s.reqs) != 3 {
		t.Fatalf("want quote, order, order; got %d", len(s.reqs))
	}
	var second map[string]any
	_ = json.Unmarshal(s.reqs[2].body, &second)
	if second["quote_id"] != "q2" || second["expected_due_now_micros"] != float64(21_500_000) {
		t.Fatalf("second order: %s", s.reqs[2].body)
	}
	if s.keys[1] == s.keys[2] || s.keys[1] != "k" {
		t.Fatalf("a re-priced order must not reuse the first idempotency key: %q", s.keys)
	}

	// Saying no to the new price buys nothing more.
	s = storeServer{}
	url = s.start(t, func(n int, r *http.Request, _ []byte) (int, string) {
		if r.URL.Path == "/v1/store/quotes" {
			return http.StatusCreated, quoteJSON("q1", 18_000_000)
		}
		return http.StatusConflict, priceChanged("q2", 21_500_000)
	})
	answers := []bool{true, false}
	b.client = api.New(url, "wmbly_test", "test")
	b.ask = func(string) (bool, error) { a := answers[0]; answers = answers[1:]; return a, nil }
	if _, err := b.run(context.Background(), []byte(`{"items":[]}`)); err != errCancelled {
		t.Fatalf("declining the new price must cancel, got %v", err)
	}
	if len(s.reqs) != 2 {
		t.Fatalf("declined buy made %d requests", len(s.reqs))
	}
}

func TestStoreBuyNamesTheCardSetupURL(t *testing.T) {
	var s storeServer
	url := s.start(t, func(n int, r *http.Request, _ []byte) (int, string) {
		if r.URL.Path == "/v1/store/quotes" {
			return http.StatusCreated, quoteJSON("q1", 18_000_000)
		}
		return http.StatusPaymentRequired, `{"error":"Payment Required","code":"payment_method_required","request_id":"r1","message":"Add a card","setup_url":"https://checkout.example/setup"}`
	})
	_, err := runCLI(t, url, append(buyArgs, "--yes")...)
	if err == nil || !strings.Contains(err.Error(), "https://checkout.example/setup") {
		t.Fatalf("want the setup URL, got %v", err)
	}
}

func TestStoreCartFlags(t *testing.T) {
	for name, c := range map[string]storeCart{
		"no provider":      {mailboxes: []string{"ada@acme.com:Ada:Lovelace"}},
		"no name":          {mailboxes: []string{"ada@acme.com"}, providers: []string{"google"}},
		"admin not listed": {mailboxes: []string{"ada@acme.com:Ada:L"}, providers: []string{"google"}, admins: []string{"bob@acme.com"}},
		"bad provider":     {mailboxes: []string{"ada@acme.com:Ada:L"}, providers: []string{"yahoo"}},
		"nothing":          {},
		"bad years":        {domains: []string{"acme.com:0"}},
	} {
		if _, err := c.items(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	// A per-domain provider wins, and mailboxes on a domain the workspace already has need no --domain.
	c := storeCart{
		domains:   []string{"new.io:2"},
		mailboxes: []string{"a@owned.com:A:B", "c@new.io:C:D"},
		providers: []string{"google", "owned.com=microsoft"},
	}
	items, err := c.items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 3 || items[0].Type != "domain" || items[0].Years != 2 || items[1].Domain != "new.io" || items[1].Provider != "google" ||
		items[2].Domain != "owned.com" || items[2].Provider != "microsoft" {
		t.Fatalf("items: %+v", items)
	}

	if _, err := (&storeCart{input: `{"items":[]}`, providers: []string{"google"}}).body(strings.NewReader("")); err == nil {
		t.Error("--input with cart flags must be refused")
	}
}

func TestStoreCommandBodies(t *testing.T) {
	t.Run("dns add", func(t *testing.T) {
		r := runAgainst(t, "store", "domains", "dns", "add", "D", "--type", "MX", "--name", "@", "--content", "mx.acme.com", "--ttl", "300", "--priority", "10")
		var rec store.DNSRecordInput
		strictDecode(t, r.body, &rec)
		if r.method != http.MethodPost || r.path != "/v1/store/domains/D/dns" || rec.Type != "MX" || rec.Name != "@" || rec.Content != "mx.acme.com" ||
			rec.TTL != 300 || rec.Priority == nil || *rec.Priority != 10 {
			t.Fatalf("sent %s %s %s", r.method, r.path, r.body)
		}
	})

	for state, want := range map[string]bool{"on": true, "off": false} {
		t.Run("auto-renew "+state, func(t *testing.T) {
			r := runAgainst(t, "store", "domains", "auto-renew", "D", state)
			var p store.DomainPatch
			strictDecode(t, r.body, &p)
			if r.method != http.MethodPatch || r.path != "/v1/store/domains/D" || p.AutoRenew == nil || *p.AutoRenew != want || p.ForwardingURL != nil {
				t.Fatalf("sent %s %s %s", r.method, r.path, r.body)
			}
		})
	}

	t.Run("forwarding", func(t *testing.T) {
		m := bodyKeys(t, runAgainst(t, "store", "domains", "forwarding", "D", "https://acme.com").body)
		if m["forwarding_url"] != "https://acme.com" || len(m) != 1 {
			t.Fatalf("sent %v", m)
		}
	})

	t.Run("connect", func(t *testing.T) {
		r := runAgainst(t, "store", "domains", "connect", "acme.com", "--method", "manual")
		var req store.ConnectRequest
		strictDecode(t, r.body, &req)
		if r.path != "/v1/store/domains/connect" || req.Domain != "acme.com" || req.Method != "manual" {
			t.Fatalf("sent %s %s", r.path, r.body)
		}
	})

	t.Run("rename", func(t *testing.T) {
		var p store.MailboxPatch
		strictDecode(t, runAgainst(t, "store", "mailboxes", "rename", "M", "--first", "Ada", "--last", "Lovelace").body, &p)
		if p.FirstName == nil || *p.FirstName != "Ada" || p.LastName == nil || *p.LastName != "Lovelace" {
			t.Fatalf("decoded %+v", p)
		}
	})

	t.Run("registrant set", func(t *testing.T) {
		r := runAgainst(t, "store", "registrant", "set", "--first-name", "Ada", "--postal-code", "N1", "--country", "GB")
		var reg store.Registrant
		strictDecode(t, r.body, &reg)
		if r.method != http.MethodPut || reg.PostalCode != "N1" || reg.Country != "GB" {
			t.Fatalf("sent %s %s", r.method, r.body)
		}
	})

	t.Run("search", func(t *testing.T) {
		r := runAgainst(t, "store", "search", "getacme", "--tlds", "com,io")
		if r.method != http.MethodGet || r.path != "/v1/store/domains/search" || r.query.Get("q") != "getacme" || r.query.Get("tlds") != "com,io" {
			t.Fatalf("sent %s %s ?%s", r.method, r.path, r.query.Encode())
		}
	})

	t.Run("cancel", func(t *testing.T) {
		if r := runAgainst(t, "store", "mailboxes", "cancel", "M"); r.method != http.MethodDelete || r.path != "/v1/store/mailboxes/M" {
			t.Fatalf("sent %s %s", r.method, r.path)
		}
	})
}

func TestStoreChargingCommandsConfirm(t *testing.T) {
	for _, args := range [][]string{
		{"store", "domains", "renew", "D"},
		{"store", "domains", "transfer-out", "D"},
		{"store", "mailboxes", "cancel", "M"},
		{"store", "mailboxes", "resume", "M"},
		{"store", "mailboxes", "sign-in", "M"},
	} {
		var s storeServer
		url := s.start(t, func(int, *http.Request, []byte) (int, string) { return http.StatusOK, `{}` })
		if _, err := runCLI(t, url, args...); err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("%v without a terminal or --yes: got %v", args, err)
		}
		if len(s.reqs) != 0 {
			t.Errorf("%v made a request before confirming", args)
		}
	}

	var s storeServer
	url := s.start(t, func(int, *http.Request, []byte) (int, string) { return http.StatusOK, `{}` })
	if _, err := runCLI(t, url, "store", "domains", "auto-renew", "D", "maybe"); err == nil || len(s.reqs) != 0 {
		t.Errorf("auto-renew must take on or off, got %v and %d requests", err, len(s.reqs))
	}
}

func TestStoreOrderWatchPollsUntilSettled(t *testing.T) {
	minWatchInterval = 0
	t.Cleanup(func() { minWatchInterval = time.Second })
	var s storeServer
	url := s.start(t, func(n int, r *http.Request, _ []byte) (int, string) {
		status := "processing"
		if n >= 3 {
			status = "completed"
		}
		return http.StatusOK, `{"id":"O","status":"` + status + `","steps":[{"key":"register","status":"done","at":null},{"key":"mailboxes","status":"running","at":null}]}`
	})
	out, err := runCLI(t, url, "store", "order", "watch", "O", "--interval", "1ms", "--json")
	if err != nil {
		t.Fatalf("watch: %v\n%s", err, out)
	}
	if len(s.reqs) != 3 || s.reqs[0].path != "/v1/store/orders/O" {
		t.Fatalf("polled %d times: %+v", len(s.reqs), s.reqs)
	}
	if !strings.Contains(out, `"completed"`) {
		t.Fatalf("the final order was not printed: %s", out)
	}

	s = storeServer{}
	url = s.start(t, func(int, *http.Request, []byte) (int, string) {
		return http.StatusOK, `{"id":"O","status":"failed","steps":[]}`
	})
	if _, err := runCLI(t, url, "store", "order", "watch", "O", "--json"); err != errSilent {
		t.Fatalf("a failed order must exit non-zero, got %v", err)
	}

	s = storeServer{}
	url = s.start(t, func(int, *http.Request, []byte) (int, string) {
		return http.StatusOK, `{"id":"O","status":"processing","steps":[]}`
	})
	if _, err := runCLI(t, url, "store", "order", "watch", "O", "--interval", "1ms", "--timeout", "30ms"); err == nil || !strings.Contains(err.Error(), "still processing") {
		t.Fatalf("a watch that times out must say the order carries on, got %v", err)
	}
}
