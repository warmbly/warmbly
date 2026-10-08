package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
)

type socketTicketStub struct{ ticket string }

func (s socketTicketStub) GenerateWebsocketToken(context.Context, uuid.UUID) (string, *errx.Error) {
	return s.ticket, nil
}

func TestSocketTicketNormalizesLegacyURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"wss://realtime.test", "wss://realtime.test/socket", "wss://realtime.test/socket/websocket/"} {
		h := &Handler{WebsocketURI: endpoint, SocketService: socketTicketStub{ticket: "short-lived"}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/getaway", nil)
		c.Set("user_id", uuid.New().String())
		h.GenerateWebsocket(c)
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["url"] != "wss://realtime.test/socket/websocket?token=short-lived" {
			t.Fatalf("legacy response: %s", w.Body)
		}
		if response["proxy_path"] != "/realtime/socket/websocket?token=short-lived" {
			t.Fatalf("missing API-origin path: %s", w.Body)
		}
	}
}

func TestSocketProxyPreservesHandshakeAndRejectsOtherCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := token.NewService(nil, nil, nil, nil, "test-secret-at-least-32-characters-long")
	userID, ticketID := uuid.New(), uuid.New()
	now := time.Now()
	ticket, err := service.GenerateTokenFor(token.PurposeWebSocket, userID, ticketID, "", "nonce", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/socket/websocket" || r.URL.Query().Get("vsn") != "1.0.0" || r.URL.Query().Get("tenant") != "legacy" || r.URL.Query().Get("target") != "" {
			t.Errorf("wrong upstream route: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Warmbly-Token") != "" {
			t.Error("forwarded unrelated credentials")
		}
		proof, xerr := service.VerifyTokenFor(token.PurposeWebSocketProxy, r.Header.Get("X-Warmbly-Proxy-Proof"))
		if xerr != nil || proof.SessionID != ticketID || proof.ClientIP != "127.0.0.1" {
			t.Error("missing authenticated peer identity")
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://dashboard.test" }}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		kind, frame, err := connection.ReadMessage()
		if err == nil {
			_ = connection.WriteMessage(kind, frame)
		}
	}))
	defer upstream.Close()
	h := &Handler{WebsocketURI: upstream.URL + "/prefix/socket?tenant=legacy", TokenService: service}
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.GET("/v1/realtime/socket/websocket", h.ProxyWebsocket)
	api := httptest.NewServer(router)
	defer api.Close()
	base := strings.Replace(api.URL, "http", "ws", 1) + "/v1/realtime/socket/websocket?vsn=1.0.0&target=https://untrusted.test&token="
	headers := http.Header{"Origin": {"https://dashboard.test"}, "Authorization": {"Bearer unrelated"}, "Cookie": {"session=unrelated"}, "X-Warmbly-Token": {"wmbly_unrelated"}, "X-Warmbly-Proxy-Proof": {"forged"}}
	connection, handshake, err := websocket.DefaultDialer.Dial(base+url.QueryEscape(ticket), headers)
	if handshake != nil {
		defer handshake.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	frame := []byte(`{"topic":"phoenix","event":"heartbeat","payload":{},"ref":"1"}`)
	if err := connection.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatal(err)
	}
	_, echoed, err := connection.ReadMessage()
	if err != nil || string(echoed) != string(frame) {
		t.Fatalf("frame relay failed: %v", err)
	}
	for _, purpose := range []string{token.PurposeAccess, token.PurposePasswordReset, token.PurposeWebSocketProxy} {
		invalid, err := service.GenerateTokenFor(purpose, userID, ticketID, "", "nonce", now, now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		_, response, err := websocket.DefaultDialer.Dial(base+url.QueryEscape(invalid), headers)
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("accepted %s credential", purpose)
		}
	}
	_, response, err := websocket.DefaultDialer.Dial(base+url.QueryEscape(ticket), http.Header{"Origin": {"https://untrusted.test"}})
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatal("lost upstream origin validation")
	}
}
