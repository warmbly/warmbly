package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

// BrokeredStore is the node-side blob backend: it holds no credential for the
// object store and asks the control plane to sign each operation instead, then
// sends the bytes straight to the store.
//
// This is what lets a worker on a machine you own carry no cloud credentials.
// The alternative is copying a long-lived access key onto every box, into a
// file `warmbly join` rewrites, where a compromised worker yields the whole
// bucket. A signed URL is one verb, one key, and expires.
//
// Bytes never pass through the backend, so the control plane pays no bandwidth
// for a mailbox sync and stays out of the data path.
//
// The endpoint contract (internal/api/handler/internal_blobs.go):
//
//	POST {BaseURL}/api/v1/internal/blobs/presign
//	  body {"op":"get|put|head|delete","key":"...","content_type":"..."}
//	  200  {"url":"...","method":"GET","expires_in":300}
//	  501  the instance's blob backend cannot sign (filesystem)
//
// Auth: Authorization: Bearer <INTERNAL_API_TOKEN>
type BrokeredStore struct {
	baseURL string
	token   string
	// broker talks to the control plane, transfer talks to the object store.
	// Two clients because the deadlines are not the same question: a presign
	// is a small local call, a transfer can legitimately be a large object on
	// a bad link.
	broker   *http.Client
	transfer *http.Client
}

// maxBrokeredBody caps what Put will buffer when the caller's reader cannot
// report its length. S3 rejects a chunked upload against a presigned URL, so
// the length has to be known before the request starts. Callers in this repo
// hand over a *bytes.Reader and never reach the buffer at all.
const maxBrokeredBody = 64 << 20

// brokerTTL is how long a signed URL stays valid. Long enough for a slow
// transfer on a bad link, short enough that a URL in a log is stale by the
// time anyone reads it.
const brokerTTL = 5 * time.Minute

// Timeouts, because the callers do not supply one. The mailbox sync loop runs
// on a context derived from context.Background(), so a request that never
// answers wedges that mailbox forever rather than failing and being retried.
const (
	brokerCallTimeout = 15 * time.Second
	transferTimeout   = 5 * time.Minute
)

// BrokeredOption configures a BrokeredStore.
type BrokeredOption func(*BrokeredStore)

// WithBrokeredHTTPClient overrides both clients, for tests and for callers
// that need a custom transport.
func WithBrokeredHTTPClient(c *http.Client) BrokeredOption {
	return func(s *BrokeredStore) { s.broker, s.transfer = c, c }
}

func NewBrokered(baseURL, token string, opts ...BrokeredOption) (*BrokeredStore, error) {
	if baseURL == "" {
		return nil, errors.New("storage.brokered: no control plane address; set ENCRYPTED_KEYS_BACKEND_URL on the backend to a URL this machine can reach, then re-join this node")
	}
	if token == "" {
		return nil, errors.New("storage.brokered: no credential; set INTERNAL_API_TOKEN (or NODE_BROKER_TOKEN) to the same value the backend uses")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("storage.brokered: invalid baseURL: %w", err)
	}
	s := &BrokeredStore{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		broker:   &http.Client{Timeout: brokerCallTimeout},
		transfer: &http.Client{Timeout: transferTimeout},
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

func (s *BrokeredStore) Name() string { return "brokered" }

type presignRequest struct {
	Op          string `json:"op"`
	Key         string `json:"key"`
	ContentType string `json:"content_type,omitempty"`
}

type presignResponse struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

// presign asks the control plane for a URL. Every operation starts here, so
// this is also where a revoked token or a backend that cannot sign surfaces,
// with the reason rather than a bare failure at transfer time.
func (s *BrokeredStore) presign(ctx context.Context, op PresignOp, key, contentType string) (presignResponse, error) {
	var out presignResponse
	if key == "" {
		return out, errors.New("storage.brokered: key is required")
	}
	body, err := json.Marshal(presignRequest{Op: string(op), Key: key, ContentType: contentType})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v1/internal/blobs/presign", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	nodeauth.Authorize(req, s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "warmbly-node/storage-brokered")

	resp, err := s.broker.Do(req)
	if err != nil {
		return out, fmt.Errorf("storage.brokered: presign %s: %w", op, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotImplemented:
		return out, fmt.Errorf("storage.brokered: %w: the instance's blob backend cannot sign URLs (BLOB_PROVIDER=filesystem)", ErrUnsupported)
	default:
		return out, fmt.Errorf("storage.brokered: presign %s: unexpected status %d", op, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("storage.brokered: decode presign: %w", err)
	}
	if out.URL == "" {
		return out, errors.New("storage.brokered: control plane returned no url")
	}
	return out, nil
}

// do runs one signed request. The response body is handed back open for Get
// and closed by every other caller.
func (s *BrokeredStore) do(ctx context.Context, signed presignResponse, body io.Reader, length int64, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, signed.Method, signed.URL, body)
	if err != nil {
		return nil, err
	}
	if length >= 0 {
		req.ContentLength = length
	}
	// The signature covers this header when it was signed with one, so it has
	// to go back exactly as asked for.
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return s.transfer.Do(req)
}

func (s *BrokeredStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	signed, err := s.presign(ctx, PresignOpGet, key, "")
	if err != nil {
		return nil, err
	}
	resp, err := s.do(ctx, signed, nil, -1, "")
	if err != nil {
		return nil, fmt.Errorf("storage.brokered: get: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		return resp.Body, nil
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound
	case http.StatusForbidden:
		// Not "missing". The URL is signed for this exact key, so an absent
		// object answers 404; a 403 means the signature expired in flight or
		// the signing principal has lost its permission. Reporting that as
		// ErrNotFound would turn a config error into "the body is gone" on
		// every send, which is the wrong thing to go looking for.
		resp.Body.Close()
		return nil, fmt.Errorf("storage.brokered: get: refused by the object store (403); the signature expired in flight or the control plane's credential no longer grants this bucket")
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("storage.brokered: get: unexpected status %d", resp.StatusCode)
	}
}

// knownLength reports the size of the readers this repo actually passes to
// Put, so the common path streams without a copy.
func knownLength(r io.Reader) (int64, bool) {
	switch v := r.(type) {
	case *bytes.Reader:
		return int64(v.Len()), true
	case *bytes.Buffer:
		return int64(v.Len()), true
	case *strings.Reader:
		return int64(v.Len()), true
	}
	return 0, false
}

func (s *BrokeredStore) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	length, ok := knownLength(body)
	if !ok {
		// A presigned PUT is signed for a plain request, and S3 rejects the
		// chunked encoding Go would otherwise choose for an unknown length.
		buf, err := io.ReadAll(io.LimitReader(body, maxBrokeredBody+1))
		if err != nil {
			return fmt.Errorf("storage.brokered: put: %w", err)
		}
		if len(buf) > maxBrokeredBody {
			return fmt.Errorf("storage.brokered: put: object exceeds %d bytes", maxBrokeredBody)
		}
		body = bytes.NewReader(buf)
		length = int64(len(buf))
	}

	signed, err := s.presign(ctx, PresignOpPut, key, contentType)
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, signed, body, length, contentType)
	if err != nil {
		return fmt.Errorf("storage.brokered: put: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("storage.brokered: put: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (s *BrokeredStore) Has(ctx context.Context, key string) (bool, error) {
	signed, err := s.presign(ctx, PresignOpHead, key, "")
	if err != nil {
		return false, err
	}
	resp, err := s.do(ctx, signed, nil, -1, "")
	if err != nil {
		return false, fmt.Errorf("storage.brokered: has: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	case http.StatusForbidden:
		// Same reasoning as Get: reporting this as "no such object" would make
		// a broken credential look like an empty bucket, and the caller would
		// happily re-store everything it already had.
		return false, fmt.Errorf("storage.brokered: has: refused by the object store (403); the signature expired in flight or the control plane's credential no longer grants this bucket")
	default:
		return false, fmt.Errorf("storage.brokered: has: unexpected status %d", resp.StatusCode)
	}
}

func (s *BrokeredStore) Delete(ctx context.Context, key string) error {
	signed, err := s.presign(ctx, PresignOpDelete, key, "")
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, signed, nil, -1, "")
	if err != nil {
		return fmt.Errorf("storage.brokered: delete: %w", err)
	}
	defer resp.Body.Close()
	// S3 answers a delete of a missing key with 204, and the Store contract
	// says that is not an error either way.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("storage.brokered: delete: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// PutPublic is not available to a node. Public objects are avatars and org
// logos, which only the backend writes, and serving them needs a public base
// URL a node has no way to know.
func (s *BrokeredStore) PutPublic(_ context.Context, _ string, _ io.Reader, _ string) (string, error) {
	return "", ErrUnsupported
}

// PresignedGetURL hands back the broker's read URL, which the control plane
// signs as a download whatever dl says.
func (s *BrokeredStore) PresignedGetURL(ctx context.Context, key string, _ time.Duration, _ Download) (string, error) {
	return s.PresignedURL(ctx, PresignOpGet, key, "", 0)
}

// PresignedURL passes the request through to the control plane. The ttl is the
// broker's to choose: a node asking for a longer-lived URL than the instance
// wants to issue is exactly what the broker exists to refuse.
func (s *BrokeredStore) PresignedURL(ctx context.Context, op PresignOp, key, contentType string, _ time.Duration) (string, error) {
	if !op.Valid() {
		return "", fmt.Errorf("storage.brokered: unknown op %q", op)
	}
	signed, err := s.presign(ctx, op, key, contentType)
	if err != nil {
		return "", err
	}
	return signed.URL, nil
}

// DeletePrefix is not available to a node. Erasure is a control-plane job, and
// a signed URL authorises one key, so there is no shape of this the broker
// could safely hand out: a node that could delete a prefix could empty the
// bucket with one call.
func (b *BrokeredStore) DeletePrefix(_ context.Context, _ string) (int, error) {
	return 0, ErrUnsupported
}
