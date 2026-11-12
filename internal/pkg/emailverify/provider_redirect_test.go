package emailverify

import (
	"context"
	"net/http"
	"testing"
)

func TestRedirectsNeverCarryAKeyOntoCleartext(t *testing.T) {
	req := func(u string) *http.Request {
		r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	https := []*http.Request{req("https://api.example.com/v1/verify")}

	// Same host, scheme downgraded: Go would keep the Authorization header.
	if err := refuseInsecureRedirect(req("http://api.example.com/v1/verify"), https); err == nil {
		t.Fatal("an https to http redirect was followed")
	}
	if err := refuseInsecureRedirect(req("https://api.example.com/v2/verify"), https); err != nil {
		t.Fatalf("an https redirect was refused: %v", err)
	}
	// A test server is reached over http to begin with; nothing is downgraded.
	plain := []*http.Request{req("http://127.0.0.1:9/v1/verify")}
	if err := refuseInsecureRedirect(req("http://127.0.0.1:9/v2/verify"), plain); err != nil {
		t.Fatalf("a redirect that downgrades nothing was refused: %v", err)
	}
	if err := refuseInsecureRedirect(req("https://api.example.com/"), make([]*http.Request, 10)); err == nil {
		t.Fatal("a redirect loop was not stopped")
	}
}

func TestEveryVerificationClientRefusesAnInsecureRedirect(t *testing.T) {
	if NewCleanMyList("key", "").client.CheckRedirect == nil {
		t.Fatal("cleanmylist follows redirects unguarded")
	}
	if NewMillionVerifier("key", "").client.CheckRedirect == nil {
		t.Fatal("millionverifier follows redirects unguarded")
	}
	if NewMailVeri("key", "").client.CheckRedirect == nil {
		t.Fatal("mailveri follows redirects unguarded")
	}
}
