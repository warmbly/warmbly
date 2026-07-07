// Package idtoken verifies provider-issued OpenID Connect ID tokens (Apple,
// Google) against the provider's published JWKS. Native apps authenticate with
// the provider on device and hand the backend an ID token; nothing about that
// token can be trusted until its signature, issuer, audience, and expiry are
// checked here.
package idtoken

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwksRefreshInterval bounds how often an unknown key id triggers a refetch,
// so a flood of forged tokens can't turn into a flood of JWKS requests.
const jwksRefreshInterval = 5 * time.Minute

// Claims are the identity assertions extracted from a verified ID token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	GivenName     string
	FamilyName    string
}

// Verifier validates RS256 ID tokens for one provider: signature via the
// provider's JWKS (cached, refreshed on rotation), then issuer and audience
// against the configured allow-lists. Safe for concurrent use.
type Verifier struct {
	jwksURL   string
	issuers   []string
	audiences []string
	client    *http.Client

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// AppleVerifier verifies Sign in with Apple identity tokens for the given
// app/bundle IDs.
func AppleVerifier(bundleIDs ...string) *Verifier {
	return NewVerifier(
		"https://appleid.apple.com/auth/keys",
		[]string{"https://appleid.apple.com"},
		bundleIDs,
	)
}

// GoogleVerifier verifies Google Sign-In ID tokens for the given OAuth client IDs.
func GoogleVerifier(clientIDs ...string) *Verifier {
	return NewVerifier(
		"https://www.googleapis.com/oauth2/v3/certs",
		[]string{"https://accounts.google.com", "accounts.google.com"},
		clientIDs,
	)
}

func NewVerifier(jwksURL string, issuers, audiences []string) *Verifier {
	return &Verifier{
		jwksURL:   jwksURL,
		issuers:   issuers,
		audiences: audiences,
		client:    &http.Client{Timeout: 10 * time.Second},
		keys:      map[string]*rsa.PublicKey{},
	}
}

// Verify checks the raw token end to end and returns its identity claims.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	tok, err := jwt.Parse(raw,
		func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			if kid == "" {
				return nil, errors.New("idtoken: missing kid header")
			}
			return v.key(ctx, kid)
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("idtoken: unexpected claims type")
	}

	iss, _ := mc["iss"].(string)
	if !contains(v.issuers, iss) {
		return nil, fmt.Errorf("idtoken: issuer %q not allowed", iss)
	}
	if !v.audienceAllowed(mc["aud"]) {
		return nil, errors.New("idtoken: audience not allowed")
	}

	sub, _ := mc["sub"].(string)
	email, _ := mc["email"].(string)
	given, _ := mc["given_name"].(string)
	family, _ := mc["family_name"].(string)

	return &Claims{
		Subject:       sub,
		Email:         email,
		EmailVerified: boolClaim(mc["email_verified"]),
		GivenName:     given,
		FamilyName:    family,
	}, nil
}

// audienceAllowed accepts the JWT aud claim as either a string or an array.
func (v *Verifier) audienceAllowed(aud any) bool {
	switch a := aud.(type) {
	case string:
		return contains(v.audiences, a)
	case []any:
		for _, entry := range a {
			if s, ok := entry.(string); ok && contains(v.audiences, s) {
				return true
			}
		}
	}
	return false
}

// key returns the cached public key for kid, refetching the JWKS at most once
// per refresh interval when the kid is unknown (provider key rotation).
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	if time.Since(v.fetched) < jwksRefreshInterval {
		return nil, fmt.Errorf("idtoken: unknown key id %q", kid)
	}
	if err := v.fetchLocked(ctx); err != nil {
		return nil, err
	}
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("idtoken: unknown key id %q", kid)
}

func (v *Verifier) fetchLocked(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("idtoken: jwks fetch returned %d", resp.StatusCode)
	}

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}

	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nb),
			E: int(new(big.Int).SetBytes(eb).Int64()),
		}
	}
	if len(keys) == 0 {
		return errors.New("idtoken: jwks contained no usable keys")
	}

	v.keys = keys
	v.fetched = time.Now()
	return nil
}

// boolClaim reads a boolean claim that Apple sometimes encodes as the strings
// "true"/"false" instead of JSON booleans.
func boolClaim(val any) bool {
	switch b := val.(type) {
	case bool:
		return b
	case string:
		return b == "true"
	}
	return false
}

func contains(list []string, s string) bool {
	for _, entry := range list {
		if entry != "" && entry == s {
			return true
		}
	}
	return false
}
