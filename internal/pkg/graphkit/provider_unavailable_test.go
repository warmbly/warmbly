package graphkit

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestUnavailableProviderIsADefinitiveRefusal(t *testing.T) {
	refused := &Error{Status: http.StatusServiceUnavailable, Code: CodeUpstreamUnavailable,
		Details: json.RawMessage(`{"provider":"microsoft","field":"items[1].provider"}`)}
	if got := refused.UnavailableProvider(); got != ProviderMicrosoft {
		t.Fatalf("provider = %q", got)
	}
	if refused.Retryable() || !Definitive(refused) {
		t.Fatal("a provider that is not offered must not be retried")
	}
	outage := &Error{Status: http.StatusServiceUnavailable, Code: CodeUpstreamUnavailable}
	if outage.UnavailableProvider() != "" || !outage.Retryable() {
		t.Fatal("an upstream outage stays retryable")
	}
}
