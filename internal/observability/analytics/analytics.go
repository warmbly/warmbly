// Package analytics posts product events to PostHog's capture API.
//
// It exists for the handful of events that have to be exact — a completed
// signup, a started subscription — where the browser is the wrong place to
// count from because an ad blocker, a closed tab or a failed request all lose
// the event that matters most.
//
// Two rules shape everything here:
//
//   - It is off unless POSTHOG_KEY is set, which is the self-host default. A
//     nil *Client is a working no-op, so callers never guard.
//   - An event names the person it happened to when the caller knows one. The
//     distinct id is the user's account id, the same id the dashboard
//     identifies the browser with, so a server-side signup and the session
//     that led to it are one person in PostHog. Person properties ride on
//     `$set`, the workspace is a group, and the originating request's IP and
//     user agent go along so the event is geolocated and attributed to the
//     right device. A caller with no person to name (a Stripe webhook for a
//     workspace with no user) falls back to the cookieless hash.
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/observability/errs"
)

// DefaultHost is PostHog Cloud US, where most of the customers are.
const DefaultHost = "https://us.i.posthog.com"

// capturePath is PostHog's single-event capture endpoint.
const capturePath = "/i/v0/e/"

// cookielessDistinctID is PostHog's sentinel telling ingestion to derive the
// visitor from the daily hash instead of from an id we supply
// (COOKIELESS_SENTINEL_VALUE in the PostHog source). Used only when the caller
// has no person to name.
const cookielessDistinctID = "$posthog_cookieless"

// OrganizationGroup is the PostHog group type a workspace is reported under.
// The dashboard uses the same name in its `group` call, so the two sides land
// on one group.
const OrganizationGroup = "organization"

// sendTimeout bounds one capture. Analytics must never be why a signup is slow.
const sendTimeout = 5 * time.Second

// Client posts events. A nil *Client is valid and does nothing.
type Client struct {
	key  string
	host string
	http *http.Client
	// warned bounds the failure log to one line per process; see warnOnce.
	warned sync.Once
}

// New returns a client, or nil when no key is configured. Returning nil rather
// than a disabled client is deliberate: it makes "analytics is off" the same
// shape as "analytics was never wired", so there is one path to test.
func New(key, host string) *Client {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		host = DefaultHost
	}
	return &Client{
		key:  key,
		host: host,
		http: &http.Client{Timeout: sendTimeout},
	}
}

// Request is who the event happened to and the browser request it came from.
type Request struct {
	// UserID is the account the event belongs to, and becomes the distinct id.
	// Empty means nobody is named and the event lands on the cookieless hash.
	UserID string
	// Email and Name are set on the person when UserID is given.
	Email string
	Name  string
	// OrganizationID puts the event in the workspace's group; OrganizationName
	// and Plan are set on that group.
	OrganizationID   string
	OrganizationName string
	Plan             string
	// SetOnce are person properties written the first time only, which is
	// what acquisition is: where somebody came from does not change later.
	SetOnce map[string]any

	// IP and UserAgent are the originating browser request, forwarded so the
	// event is geolocated and, without a UserID, so the cookieless hash lands
	// on the same visitor as that browser's own events.
	IP        string
	UserAgent string
	// Host is the site the visitor was on, one of the cookieless hash inputs.
	// PostHog reduces it to the registrable root domain, so app.warmbly.com
	// and warmbly.com hash alike.
	Host string
}

// Capture sends one event.
//
// It sends in the background and reports its own failures rather than
// returning them: no caller should abandon a signup because an analytics host
// was unreachable.
func (c *Client) Capture(name string, req Request, properties map[string]any) {
	if c == nil || name == "" {
		return
	}

	props := map[string]any{}
	for k, v := range properties {
		props[k] = v
	}
	if req.IP != "" {
		props["$ip"] = req.IP
	}
	if req.UserAgent != "" {
		props["$raw_user_agent"] = req.UserAgent
	}
	if req.Host != "" {
		props["$host"] = req.Host
	}

	distinctID := cookielessDistinctID
	if req.UserID != "" {
		distinctID = req.UserID
		set := map[string]any{}
		if req.Email != "" {
			set["email"] = req.Email
		}
		if req.Name != "" {
			set["name"] = req.Name
		}
		if len(set) > 0 {
			props["$set"] = set
		}
		if len(req.SetOnce) > 0 {
			props["$set_once"] = req.SetOnce
		}
		if req.OrganizationID != "" {
			props["$groups"] = map[string]any{OrganizationGroup: req.OrganizationID}
		}
	} else {
		// The flag ingestion keys on (COOKIELESS_MODE_FLAG_PROPERTY). PostHog
		// deletes $ip and $raw_user_agent from the event once hashed.
		props["$cookieless_mode"] = true
	}

	body, err := json.Marshal(map[string]any{
		"api_key":     c.key,
		"event":       name,
		"distinct_id": distinctID,
		"properties":  props,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		errs.CaptureException(err)
		return
	}

	go c.post(body)

	if req.UserID != "" && req.OrganizationID != "" {
		c.identifyGroup(req)
	}
}

// identifyGroup sets the workspace's group properties. PostHog takes them on
// a `$groupidentify` event rather than on the event itself.
func (c *Client) identifyGroup(req Request) {
	set := map[string]any{}
	if req.OrganizationName != "" {
		set["name"] = req.OrganizationName
	}
	if req.Plan != "" {
		set["plan"] = req.Plan
	}
	if len(set) == 0 {
		return
	}
	body, err := json.Marshal(map[string]any{
		"api_key":     c.key,
		"event":       "$groupidentify",
		"distinct_id": req.UserID,
		"properties": map[string]any{
			"$group_type": OrganizationGroup,
			"$group_key":  req.OrganizationID,
			"$group_set":  set,
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		errs.CaptureException(err)
		return
	}
	go c.post(body)
}

func (c *Client) post(body []byte) {
	defer func() {
		if r := recover(); r != nil {
			errs.Recover(r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+capturePath, bytes.NewReader(body))
	if err != nil {
		c.warnOnce("cannot build a capture request for %s: %v", c.host, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		c.warnOnce("cannot reach the analytics host %s: %v", c.host, err)
		return
	}
	defer resp.Body.Close()
	// Drained so the connection can be reused; the response body is of no
	// interest beyond that.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.warnOnce("the analytics host %s rejected a capture with %s (check POSTHOG_KEY)", c.host, resp.Status)
	}
}

// warnOnce logs the first failure and nothing after it.
//
// A wrong key or an unreachable host fails on every single event, so logging
// each one would bury the instance's real logs under analytics noise. Logging
// none of them is worse: a misconfigured key would look exactly like a quiet
// week. One line, the first time, is the useful amount.
func (c *Client) warnOnce(format string, args ...any) {
	c.warned.Do(func() {
		log.Printf("product analytics disabled for this run: "+format, args...)
	})
}
