package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

// BrokeredProvider is the node-side KMS: it holds no key material and no cloud
// credential, and asks the control plane to open a sealed DEK for it.
//
// The point is what a worker then needs to run. With KMS_PROVIDER=aws a node
// must carry an IAM credential good for kms:Decrypt, which means a long-lived
// access key on every machine, in a file `warmbly join` rewrites. Brokering the
// one operation a node actually performs replaces that with the internal API
// token it already has, which is instance-scoped and revocable.
//
// This is not a weaker boundary than the alternative. A node already holds
// CREDENTIALS_ENCRYPTION_KEY and caches plaintext DEKs in Redis: it is trusted
// with plaintext by design, because it is the process that talks to a
// customer's mailbox. What changes is that it is no longer also trusted with a
// credential for the whole account.
//
// The endpoint contract (internal/api/handler/internal_dek.go):
//
//	POST {BaseURL}/api/v1/internal/dek/decrypt
//	  body {"encrypted_data_key":"<base64>"}
//	  200  {"data_key":"<base64 plaintext>"}
//
// Auth: Authorization: Bearer <INTERNAL_API_TOKEN>
type BrokeredProvider struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewBrokered builds the provider. baseURL is the control plane, token is the
// internal API token both sides share.
func NewBrokered(baseURL, token string) (*BrokeredProvider, error) {
	if baseURL == "" {
		return nil, errors.New("kms.brokered: no control plane address; set ENCRYPTED_KEYS_BACKEND_URL on the backend to a URL this machine can reach, then re-join this node")
	}
	if token == "" {
		return nil, errors.New("kms.brokered: no credential; set INTERNAL_API_TOKEN (or NODE_BROKER_TOKEN) to the same value the backend uses")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("kms.brokered: invalid baseURL: %w", err)
	}
	return &BrokeredProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (p *BrokeredProvider) Name() string { return "brokered" }

// GenerateDataKey is deliberately unavailable. A node only ever handles
// organizations whose key already exists, minted by the control plane when
// their first secret was stored. A node reaching this call means it was asked
// to encrypt for an organization it should never have seen, and creating a key
// there would race the control plane for which one gets stored.
func (p *BrokeredProvider) GenerateDataKey(_ context.Context) ([]byte, string, error) {
	return nil, "", errors.New("kms.brokered: a node does not mint data keys; the control plane creates an organization's key when its first secret is stored")
}

type decryptRequest struct {
	EncryptedDataKey string `json:"encrypted_data_key"`
}

type decryptResponse struct {
	DataKey string `json:"data_key"`
}

func (p *BrokeredProvider) GetDecryptedKey(ctx context.Context, ciphertextB64 string) ([]byte, error) {
	if ciphertextB64 == "" {
		return nil, errors.New("kms.brokered: empty ciphertext")
	}
	body, err := json.Marshal(decryptRequest{EncryptedDataKey: ciphertextB64})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/v1/internal/dek/decrypt", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	nodeauth.Authorize(req, p.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "warmbly-node/kms-brokered")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kms.brokered: decrypt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kms.brokered: decrypt: unexpected status %d", resp.StatusCode)
	}
	var out decryptResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("kms.brokered: decode: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(out.DataKey)
	if err != nil {
		return nil, fmt.Errorf("kms.brokered: decode data key: %w", err)
	}
	// AES-256 everywhere in this system. A short key here means the control
	// plane answered with something that is not a DEK, and failing now beats
	// sealing data with it.
	if len(key) != 32 {
		return nil, fmt.Errorf("kms.brokered: data key is %d bytes, want 32", len(key))
	}
	return key, nil
}
