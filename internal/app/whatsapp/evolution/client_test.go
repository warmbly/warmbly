package evolution

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
)

func TestSendTextSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "test-key" {
			t.Errorf("missing apikey")
		}
		if r.URL.Path != "/message/sendText/inst1" {
			t.Errorf("path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "5548999999999") {
			t.Errorf("body=%s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":{"id":"ABC123"},"status":"PENDING"}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: "test-key", Timeout: 2 * time.Second, MaxRetries: 0})
	p := NewProvider(c, "inst1")
	res, err := p.SendText(context.Background(), whatsapp.SendTextRequest{
		ToE164: "+5548999999999",
		Body:   "Olá",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMessageID != "ABC123" {
		t.Fatalf("id=%s", res.ProviderMessageID)
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	codes := []int{400, 401, 403, 404, 429, 500}
	for _, code := range codes {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":"x"}`))
			}))
			defer srv.Close()
			c := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: "k", Timeout: time.Second, MaxRetries: 0})
			_, err := c.SendText(context.Background(), "i", "55", "hi")
			if err == nil {
				t.Fatal("expected error")
			}
			ae, ok := err.(*APIError)
			if !ok {
				t.Fatalf("type %T", err)
			}
			if ae.StatusCode != code {
				t.Fatalf("status=%d", ae.StatusCode)
			}
			retryable := code == 429 || code == 500
			if ae.Retryable != retryable {
				t.Fatalf("retryable=%v want %v", ae.Retryable, retryable)
			}
			// secret redaction: error string must not contain api key even if body did
			if strings.Contains(ae.Error(), "super-secret-key") {
				t.Fatal("leaked key")
			}
		})
	}
}

func TestRetryOn500ThenSuccess(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`fail`))
			return
		}
		_, _ = w.Write([]byte(`{"key":{"id":"OK"},"status":"PENDING"}`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: "k", Timeout: time.Second, MaxRetries: 2})
	res, err := c.SendText(context.Background(), "i", "55", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Key.ID != "OK" {
		t.Fatal(res.Key.ID)
	}
	if n.Load() < 2 {
		t.Fatalf("attempts=%d", n.Load())
	}
}

func TestNoRetryOn400(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`bad`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: "k", Timeout: time.Second, MaxRetries: 3})
	_, err := c.SendText(context.Background(), "i", "55", "hi")
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("should not retry permanent errors, attempts=%d", n.Load())
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: "k", Timeout: 50 * time.Millisecond, MaxRetries: 0})
	_, err := c.SendText(context.Background(), "i", "55", "hi")
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestOffline(t *testing.T) {
	c := NewClient(ClientConfig{BaseURL: "http://127.0.0.1:1", APIKey: "k", Timeout: 100 * time.Millisecond, MaxRetries: 0})
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestRedactAPIKey(t *testing.T) {
	s := RedactAPIKey("failed key=super-secret-key-xyz", "super-secret-key-xyz")
	if strings.Contains(s, "super-secret-key-xyz") {
		t.Fatal(s)
	}
}

func TestNormalizeWebhookInbound(t *testing.T) {
	raw := []byte(`{
		"event": "messages.upsert",
		"instance": "confenge",
		"data": {
			"key": {"remoteJid": "5548999887766@s.whatsapp.net", "fromMe": false, "id": "MSG1"},
			"message": {"conversation": "Olá, quero falar no WhatsApp"},
			"messageTimestamp": 1710000000
		}
	}`)
	ev, err := NormalizeWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventType != whatsapp.EventMessageReceived {
		t.Fatalf("type=%s", ev.EventType)
	}
	if ev.FromE164 != "+5548999887766" {
		t.Fatalf("from=%s", ev.FromE164)
	}
	if ev.Content.Text != "Olá, quero falar no WhatsApp" {
		t.Fatalf("text=%s", ev.Content.Text)
	}
	if ev.Provider != whatsapp.ProviderEvolution {
		t.Fatal(ev.Provider)
	}
}

func TestNormalizeUnsupported(t *testing.T) {
	ev, err := NormalizeWebhook([]byte(`{"event":"groups.update","instance":"x","data":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventType != whatsapp.EventUnsupported {
		t.Fatal(ev.EventType)
	}
}
