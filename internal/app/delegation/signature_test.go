package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSetPurchasedSignaturePatchesTheMailboxsOwnSendAs(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	broker := &brokerStub{}
	s.WirePurchasedTokens(broker)
	var path, auth, sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		sig = body["signature"]
		writeJSON(w, map[string]any{"sendAsEmail": "ada@getacme.com"})
	}))
	t.Cleanup(srv.Close)
	s.gmailBase = srv.URL

	if err := s.SetPurchasedSignature(context.Background(), uuid.New(), "Ada@getacme.com", "<p><b>Ada</b></p>", true); err != nil {
		t.Fatal(err)
	}
	if path != "/users/me/settings/sendAs/ada@getacme.com" || auth != "Bearer brokered-ada@getacme.com" || sig != "<p><b>Ada</b></p>" {
		t.Fatalf("patched %q with %q: %q", path, auth, sig)
	}
	if last := broker.calls[len(broker.calls)-1]; !strings.HasSuffix(last, "https://www.googleapis.com/auth/gmail.settings.basic") {
		t.Fatalf("token asked for %q", last)
	}
}

func TestSetPurchasedSignatureReportsARefusalAsFinal(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	s.WirePurchasedTokens(&brokerStub{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) }))
	t.Cleanup(srv.Close)
	s.gmailBase = srv.URL
	if err := s.SetPurchasedSignature(context.Background(), uuid.New(), "ada@getacme.com", "<p>x</p>", true); !errors.Is(err, ErrSignatureRefused) {
		t.Fatalf("err = %v", err)
	}
	// The development simulator's tokens never reach Google.
	s.WirePurchasedTokens(&brokerStub{simulated: true})
	if err := s.SetPurchasedSignature(context.Background(), uuid.New(), "ada@getacme.com", "<p>x</p>", true); err != nil {
		t.Fatalf("simulated: %v", err)
	}
}
