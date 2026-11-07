package slackapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlackVerifyCallbackOriginIsStateBound(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	s := &Service{guard: newGuard(nil)}
	ctx := context.Background()
	blob, _ := json.Marshal(verifyState{ReturnOrigin: "https://second.example.com"})
	s.guard.put(ctx, verifyKey("state"), string(blob), time.Minute)
	if s.OAuthReturnOrigin(ctx, "state") != "https://second.example.com" || s.guard.get(ctx, verifyKey("state")) == "" {
		t.Fatal("routing must preserve verification state")
	}
	if s.OAuthReturnOrigin(ctx, "unknown") != "" {
		t.Fatal("unknown state must not route")
	}
	s.guard.del(ctx, verifyKey("state"))
	if s.OAuthReturnOrigin(ctx, "state") != "" {
		t.Fatal("consumed state must not route")
	}
	for _, origin := range []string{"https://evil.example.com", "https://second.example.com/path"} {
		blob, _ := json.Marshal(verifyState{ReturnOrigin: origin})
		s.guard.put(ctx, verifyKey("state"), string(blob), time.Minute)
		if s.OAuthReturnOrigin(ctx, "state") != "" {
			t.Fatal("untrusted origin must not route")
		}
	}
}

// A verification state is taken at most once, however many callbacks race for it.
func TestVerifyStateIsTakenOnce(t *testing.T) {
	g := newGuard(nil)
	ctx := context.Background()
	g.put(ctx, verifyKey("state"), "v", time.Minute)

	var wg sync.WaitGroup
	var got atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if g.take(ctx, verifyKey("state")) == "v" {
				got.Add(1)
			}
		}()
	}
	wg.Wait()
	if got.Load() != 1 {
		t.Fatalf("state taken %d times, want 1", got.Load())
	}
	if g.take(ctx, verifyKey("state")) != "" || g.get(ctx, verifyKey("state")) != "" {
		t.Fatal("a taken state must be gone")
	}

	g.put(ctx, verifyKey("expired"), "v", -time.Second)
	if g.take(ctx, verifyKey("expired")) != "" {
		t.Fatal("an expired state must not be taken")
	}
}

func idToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	b, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func TestIDTokenClaims(t *testing.T) {
	now := time.Now()
	base := func() map[string]any {
		return map[string]any{
			"iss":                       oidcIssuer,
			"aud":                       "client-1",
			"exp":                       now.Add(time.Minute).Unix(),
			"nonce":                     "n1",
			"https://slack.com/team_id": "T1",
			"https://slack.com/user_id": "U1",
		}
	}
	c, err := parseIDToken(idToken(t, base()))
	if err != nil {
		t.Fatal(err)
	}
	if !c.valid("client-1", "n1", now) || c.TeamID != "T1" || c.UserID != "U1" {
		t.Fatalf("a well-formed token must be valid, got %+v", c)
	}

	list := base()
	list["aud"] = []string{"other", "client-1"}
	if c, err := parseIDToken(idToken(t, list)); err != nil || !c.valid("client-1", "n1", now) {
		t.Fatal("an aud list containing the client must be accepted")
	}

	bad := map[string]func(map[string]any){
		"issuer":  func(m map[string]any) { m["iss"] = "https://evil.example" },
		"aud":     func(m map[string]any) { m["aud"] = "other" },
		"expired": func(m map[string]any) { m["exp"] = now.Add(-time.Second).Unix() },
		"nonce":   func(m map[string]any) { m["nonce"] = "n2" },
		"no team": func(m map[string]any) { delete(m, "https://slack.com/team_id") },
		"no user": func(m map[string]any) { delete(m, "https://slack.com/user_id") },
	}
	for name, mutate := range bad {
		m := base()
		mutate(m)
		c, err := parseIDToken(idToken(t, m))
		if err == nil && c.valid("client-1", "n1", now) {
			t.Errorf("%s: token must be refused", name)
		}
	}
	if c.valid("client-1", "", now) {
		t.Error("an empty expected nonce must never match")
	}
	for _, tok := range []string{"", "a.b", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".c"} {
		if _, err := parseIDToken(tok); err == nil {
			t.Errorf("parseIDToken(%q) must fail", tok)
		}
	}
}
