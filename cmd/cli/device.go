package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/version"
)

// The browser half of `warmbly auth login`, RFC 8628 shaped: ask for a code,
// show it, wait for a member to approve it in the browser, collect the key.

type deviceStart struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_uri"`
	VerificationURLComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type devicePoll struct {
	Status           string   `json:"status"`
	Token            string   `json:"token"`
	APIKeyID         string   `json:"api_key_id"`
	ScopeNames       []string `json:"scope_names"`
	UserID           string   `json:"user_id"`
	UserEmail        string   `json:"user_email"`
	UserName         string   `json:"user_name"`
	OrganizationID   string   `json:"organization_id"`
	OrganizationName string   `json:"organization_name"`
}

// startDeviceFlow opens the handshake. The client is anonymous: there is
// nothing to authenticate with yet.
func startDeviceFlow(ctx context.Context, client *api.Client, hostname string, scopes uint64) (*deviceStart, error) {
	body, err := json.Marshal(models.CLIAuthStartRequest{
		ClientName: "Warmbly CLI",
		Hostname:   hostname,
		CLIVersion: version.String(),
		Scopes:     scopes,
	})
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(ctx, api.Request{Method: http.MethodPost, Path: "/auth/cli/code", Body: body, Anonymous: true})
	if err != nil {
		if api.StatusOf(err) == http.StatusNotFound || api.StatusOf(err) == http.StatusNotImplemented {
			return nil, fmt.Errorf("%s does not support browser sign-in for the CLI.\nUse `warmbly auth login --with-token` with an API key from Settings > API keys instead.", client.BaseURL)
		}
		return nil, err
	}
	var out deviceStart
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, fmt.Errorf("the sign-in handshake returned something unexpected: %w", err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return nil, errors.New("the sign-in handshake returned no code")
	}
	if out.Interval <= 0 {
		out.Interval = 3
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 600
	}
	return &out, nil
}

// pollDeviceFlow waits for the browser half. It stops on approval, denial,
// expiry, or the context being cancelled, and never faster than the interval
// the server asked for.
func pollDeviceFlow(ctx context.Context, client *api.Client, start *deviceStart) (*devicePoll, error) {
	body, err := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	if err != nil {
		return nil, err
	}
	interval := time.Duration(start.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the code %s expired before it was approved. Run `warmbly auth login` again.", start.UserCode)
		}

		resp, err := client.Do(ctx, api.Request{Method: http.MethodPost, Path: "/auth/cli/poll", Body: body, Anonymous: true})
		if err != nil {
			// The code is gone: expired, or someone else claimed it.
			if api.StatusOf(err) == http.StatusNotFound {
				return nil, fmt.Errorf("the code %s is no longer valid. Run `warmbly auth login` again.", start.UserCode)
			}
			// A rate limit or a blip must not end a sign-in someone is
			// standing at; back off and keep waiting.
			if api.StatusOf(err) == http.StatusTooManyRequests || api.StatusOf(err) == 0 {
				interval += time.Second
				continue
			}
			return nil, err
		}

		var out devicePoll
		if err := json.Unmarshal(resp.Body, &out); err != nil {
			return nil, err
		}
		switch out.Status {
		case string(models.CLIAuthCodeApproved):
			if out.Token == "" {
				return nil, errors.New("the approval returned no token. Run `warmbly auth login` again.")
			}
			return &out, nil
		case string(models.CLIAuthCodeDenied):
			return nil, errors.New("the request was declined in the browser. Nothing was created.")
		case string(models.CLIAuthCodeClaimed):
			return nil, errors.New("that code was already used. Run `warmbly auth login` again.")
		}
	}
}

// openBrowser opens a URL, honouring the browser config key and then BROWSER.
// A failure is never fatal: the URL has already been printed.
func openBrowser(cfg *config.Config, url string) error {
	if custom := strings.TrimSpace(cfg.Browser); custom != "" {
		parts, err := splitArgs(custom)
		if err != nil || len(parts) == 0 {
			return fmt.Errorf("the browser config key is not a usable command")
		}
		return exec.Command(parts[0], append(parts[1:], url)...).Start()
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
