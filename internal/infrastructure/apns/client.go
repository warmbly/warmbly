// Package apns is a minimal APNs provider: token-based (p8) auth over the
// HTTP/2 API, no third-party APNs dependency. The stdlib http.Transport
// negotiates HTTP/2 with api.push.apple.com automatically.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrUnregistered means APNs no longer knows the device token (app deleted,
// token rotated). Callers should drop the token from storage.
var ErrUnregistered = errors.New("apns: device token unregistered")

const (
	hostProduction  = "https://api.push.apple.com"
	hostDevelopment = "https://api.sandbox.push.apple.com"
	// Apple wants provider tokens refreshed every 20-60 minutes.
	tokenTTL = 45 * time.Minute
)

// Notification is one alert push.
type Notification struct {
	Title string
	Body  string
	// Badge, when non-nil, sets the app icon badge count.
	Badge *int
	// ThreadID groups alerts in the notification center (we use the category).
	ThreadID string
	// CollapseID makes a later push replace an earlier one with the same id.
	CollapseID string
	// Custom keys delivered alongside aps (link, category, notification id).
	Custom map[string]any
}

// Client sends alert pushes with a cached ES256 provider token.
type Client struct {
	key    *ecdsa.PrivateKey
	keyID  string
	teamID string
	topic  string
	http   *http.Client

	mu       sync.Mutex
	bearer   string
	bearerAt time.Time
}

// FromEnv builds a client from APNS_KEY (PEM contents) or APNS_KEY_PATH, plus
// APNS_KEY_ID, APNS_TEAM_ID, and APNS_TOPIC (the app bundle id). Returns nil
// when unconfigured so callers can treat push as absent.
func FromEnv() (*Client, error) {
	keyPEM := os.Getenv("APNS_KEY")
	if keyPEM == "" {
		if path := os.Getenv("APNS_KEY_PATH"); path != "" {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("apns: read key: %w", err)
			}
			keyPEM = string(raw)
		}
	}
	keyID := os.Getenv("APNS_KEY_ID")
	teamID := os.Getenv("APNS_TEAM_ID")
	topic := os.Getenv("APNS_TOPIC")
	if keyPEM == "" && keyID == "" && teamID == "" && topic == "" {
		return nil, nil // push not configured
	}
	if keyPEM == "" || keyID == "" || teamID == "" || topic == "" {
		return nil, errors.New("apns: partial config; need APNS_KEY (or APNS_KEY_PATH), APNS_KEY_ID, APNS_TEAM_ID, APNS_TOPIC")
	}
	key, err := parseP8(keyPEM)
	if err != nil {
		return nil, err
	}
	return &Client{
		key:    key,
		keyID:  keyID,
		teamID: teamID,
		topic:  topic,
		http:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func parseP8(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("apns: key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: key is not ECDSA (expected an Apple .p8)")
	}
	return key, nil
}

func (c *Client) providerToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bearer != "" && time.Since(c.bearerAt) < tokenTTL {
		return c.bearer, nil
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.teamID,
		"iat": time.Now().Unix(),
	})
	tok.Header["kid"] = c.keyID
	signed, err := tok.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("apns: sign provider token: %w", err)
	}
	c.bearer = signed
	c.bearerAt = time.Now()
	return signed, nil
}

// Push delivers one alert to a device token. environment selects the sandbox
// or production APNs host (per-token, matching how the device was built).
func (c *Client) Push(ctx context.Context, deviceToken, environment string, n Notification) error {
	bearer, err := c.providerToken()
	if err != nil {
		return err
	}

	aps := map[string]any{
		"alert": map[string]string{"title": n.Title, "body": n.Body},
		"sound": "default",
	}
	if n.Badge != nil {
		aps["badge"] = *n.Badge
	}
	if n.ThreadID != "" {
		aps["thread-id"] = n.ThreadID
	}
	payload := map[string]any{"aps": aps}
	for k, v := range n.Custom {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	host := hostProduction
	if environment == "development" {
		host = hostDevelopment
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+deviceToken, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", c.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	if n.CollapseID != "" {
		req.Header.Set("apns-collapse-id", n.CollapseID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var apiErr struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(raw, &apiErr)
	if resp.StatusCode == http.StatusGone ||
		apiErr.Reason == "Unregistered" || apiErr.Reason == "BadDeviceToken" || apiErr.Reason == "DeviceTokenNotForTopic" {
		return ErrUnregistered
	}
	return fmt.Errorf("apns: %d %s", resp.StatusCode, strings.TrimSpace(apiErr.Reason))
}
