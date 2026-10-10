package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var remoteUserCode = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)
var remoteDeviceSecret = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type remoteDeviceGrant struct {
	DeviceSecret     string `json:"device_secret"`
	UserCode         string `json:"user_code"`
	InstanceURL      string `json:"instance_url"`
	VerificationPath string `json:"verification_path"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
}

func validateRemoteGrant(grant remoteDeviceGrant, base string) error {
	instance, err := normalizeRemoteURL(grant.InstanceURL)
	if err != nil || instance != base || !remoteDeviceSecret.MatchString(grant.DeviceSecret) || !remoteUserCode.MatchString(grant.UserCode) || grant.VerificationPath != "/device" || grant.ExpiresIn < 1 || grant.ExpiresIn > 300 || grant.Interval < 5 || grant.Interval > 60 {
		return remoteFailure(4, "device_contract_mismatch", "Device response did not match the selected instance or supported contract. Nothing was approved or stored.")
	}
	return nil
}

func runRemoteLogin(ctx context.Context, args []string) error {
	fs := newRemoteFlagSet("login")
	var sel remoteSelection
	sel.flags(fs)
	name := fs.String("client-name", "warmblyctl", "Requester identity displayed to the approving administrator")
	if err := parseRemoteFlags(fs, args); err != nil {
		return err
	}
	if len(*name) < 1 || len(*name) > 120 || strings.IndexFunc(*name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return remoteFailure(2, "invalid_client_name", "Use a client name of 1 to 120 bytes without control characters.")
	}
	path, err := remoteStorePath()
	if err != nil {
		return err
	}
	store, err := openRemoteStore(ctx, path)
	if err != nil {
		return err
	}
	base, profile, err := sel.resolve(store)
	if err != nil {
		store.close()
		return err
	}
	key := remoteCredentialKey(base, profile)
	_, exists := store.state.Credentials[key]
	store.close()
	if exists {
		return remoteFailure(2, "already_logged_in", "This profile already has a session. Logout before a new explicit approval; select another named profile to keep both sessions.")
	}
	body, _ := json.Marshal(map[string]string{"client_name": *name})
	client := remoteHTTPClient()
	payload, err := remoteRequest(ctx, client, base, "POST", "/v1/auth/cli/admin/code", "", body)
	if err != nil {
		return err
	}
	var grant remoteDeviceGrant
	if json.Unmarshal(payload, &grant) != nil {
		return remoteFailure(5, "invalid_response", "Could not decode the device grant. No raw response was printed.")
	}
	if err := validateRemoteGrant(grant, base); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Instance: %s\nClient: %s\nOpen your configured administrator website at %s on any device, sign in with MFA, and enter %s. Explicit approval is required. Expires in %d seconds.\n", base, *name, grant.VerificationPath, grant.UserCode, grant.ExpiresIn)
	// No browser, HTTP listener, secret URL or password input is involved.
	deadline, cancel := context.WithTimeout(ctx, time.Duration(grant.ExpiresIn)*time.Second)
	defer cancel()
	interval := time.Duration(grant.Interval) * time.Second
	for {
		timer := time.NewTimer(interval)
		select {
		case <-deadline.Done():
			timer.Stop()
			return remoteFailure(4, "device_expired", "Device approval expired or was cancelled. Start a new login.")
		case <-timer.C:
		}
		pollBody, _ := json.Marshal(map[string]string{"device_secret": grant.DeviceSecret})
		payload, err := remoteRequest(deadline, client, base, "POST", "/v1/auth/cli/admin/poll", "", pollBody)
		if err != nil {
			var rate *remoteError
			if errors.As(err, &rate) && rate.Status == 429 {
				if rate.Retry > grant.Interval {
					interval = time.Duration(rate.Retry) * time.Second
				}
				continue
			}
			return err // A lost issuance response cannot be safely replayed.
		}
		var result struct {
			Status string      `json:"status"`
			UserID string      `json:"user_id"`
			Token  remoteToken `json:"token"`
		}
		if json.Unmarshal(payload, &result) != nil {
			return remoteFailure(5, "invalid_response", "Could not decode approval status. Start a new login; no raw response was printed.")
		}
		switch result.Status {
		case "pending":
			continue
		case "denied":
			return remoteFailure(3, "device_denied", "Administrator denied the device request. No credentials were stored.")
		case "approved":
			if _, err := uuid.Parse(result.UserID); err != nil || !result.Token.valid(time.Now()) {
				return remoteFailure(4, "device_token_invalid", "Approval returned an invalid session. Start a new login; no credentials were stored.")
			}
			// A second process may have logged in while the human was approving.
			store, err = openRemoteStore(ctx, path)
			if err == nil {
				defer store.close()
				resolved, _, selectErr := sel.resolve(store)
				if selectErr != nil || resolved != base {
					err = remoteFailure(2, "server_mismatch", "Profile changed during approval. The new session was not stored.")
				} else if _, ok := store.state.Credentials[key]; ok {
					err = remoteFailure(2, "already_logged_in", "Another process logged into this profile. The new session was not stored.")
				} else {
					store.state.Credentials[key] = remoteCredential{URL: base, UserID: result.UserID, Token: result.Token}
					if sel.Server != "" {
						store.state.Servers[profile] = base
					}
					err = store.save()
				}
			}
			if err != nil {
				_, revokeErr := remoteRequest(ctx, client, base, "POST", "/v1/auth/logout", result.Token.AccessToken, nil)
				if revokeErr != nil {
					fmt.Fprintln(os.Stderr, "The unstored session could not be confirmed revoked. Revoke it from your administrator session settings.")
				}
				return err
			}
			return writeRemoteJSON(os.Stdout, map[string]any{"status": "logged_in", "instance_url": base, "server": profile, "user_id": result.UserID, "access_token_expires_at": result.Token.AccessTokenExpiresAt, "refresh_token_expires_at": result.Token.RefreshTokenExpiresAt})
		default:
			return remoteFailure(5, "invalid_response", "Unknown approval status. Start a new login.")
		}
	}
}

func runRemoteWhoami(ctx context.Context, args []string) error {
	fs := newRemoteFlagSet("whoami")
	var sel remoteSelection
	sel.flags(fs)
	if err := parseRemoteFlags(fs, args); err != nil {
		return err
	}
	client, err := newRemoteClient(ctx, sel)
	if err != nil {
		return err
	}
	defer client.store.close()
	payload, err := client.do(ctx, "GET", "/v1/auth/me", nil)
	if err != nil {
		return err
	}
	return printRemoteResponse(payload)
}

func remoteWriteFlags(fs *flag.FlagSet) (*bool, *string) {
	return fs.Bool("write", false, "Explicitly authorize a mutating operation"), fs.String("confirm", "", "Repeat the exact METHOD /admin/path (without query) to confirm the operation and target")
}

func requireRemoteWrite(method, path string, write bool, confirmation string) error {
	if !write || confirmation != method+" "+path {
		return remoteFailure(8, "confirmation_required", "Writes require --write and --confirm 'METHOD /admin/exact-target-path'. No request was sent.")
	}
	return nil
}

func runRemoteLogout(ctx context.Context, args []string) error {
	fs := newRemoteFlagSet("logout")
	var sel remoteSelection
	sel.flags(fs)
	all := fs.Bool("all", false, "Revoke all sessions for this user on this backend; requires explicit confirmation")
	local := fs.Bool("local-only", false, "Forget this local credential without remote revocation")
	write, confirm := remoteWriteFlags(fs)
	if err := parseRemoteFlags(fs, args); err != nil {
		return err
	}
	if *local && *all {
		return remoteFailure(2, "invalid_arguments", "--local-only and --all cannot be combined.")
	}
	if *all {
		if err := requireRemoteWrite("POST", "/v1/auth/logout-all", *write, *confirm); err != nil {
			return err
		}
	}
	client, err := newRemoteClient(ctx, sel)
	if err != nil {
		return err
	}
	defer client.store.close()
	if !*local {
		path := "/v1/auth/logout"
		if *all {
			path = "/v1/auth/logout-all"
		}
		if _, err := client.do(ctx, "POST", path, nil); err != nil {
			return err
		}
	}
	if *all {
		for key, cred := range client.store.state.Credentials {
			if cred.URL == client.base && cred.UserID == client.credential.UserID {
				delete(client.store.state.Credentials, key)
			}
		}
	}
	if err := client.forget(); err != nil {
		return err
	}
	if *local {
		fmt.Fprintln(os.Stderr, "Only the local credential was forgotten. The server session remains valid until separately revoked or expired.")
	}
	return writeRemoteJSON(os.Stdout, map[string]any{"status": "logged_out", "server": client.profile, "instance_url": client.base, "revoked": !*local, "all_sessions": *all})
}

func runRemote(ctx context.Context, command string, args []string) error {
	switch command {
	case "login":
		return runRemoteLogin(ctx, args)
	case "logout":
		return runRemoteLogout(ctx, args)
	case "whoami":
		return runRemoteWhoami(ctx, args)
	case "admin":
		return runRemoteAdmin(ctx, args)
	}
	return flag.ErrHelp
}
