package config

import "testing"

// Clients dial whatever GET /v1/auth/config advertises, so every form an
// operator plausibly writes has to normalise to the Phoenix transport path.
func TestWebsocketURLNormalisation(t *testing.T) {
	cases := map[string]string{
		"wss://ws.example.com":                  "wss://ws.example.com/socket/websocket",
		"wss://ws.example.com/":                 "wss://ws.example.com/socket/websocket",
		"wss://ws.example.com/socket":           "wss://ws.example.com/socket/websocket",
		"wss://ws.example.com/socket/":          "wss://ws.example.com/socket/websocket",
		"wss://ws.example.com/socket/websocket": "wss://ws.example.com/socket/websocket",
		"ws://localhost:4000/socket/websocket":  "ws://localhost:4000/socket/websocket",
		"":                                      "",
	}
	for in, want := range cases {
		t.Setenv("WEBSOCKET_URL", in)
		if got := WebsocketURL(); got != want {
			t.Errorf("WebsocketURL(%q) = %q, want %q", in, got, want)
		}
	}
}
