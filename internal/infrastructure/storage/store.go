package storage

import (
	"context"
	"io"
	"time"
)

// Store is the abstraction over blob storage. One implementation suffices for
// AWS S3, MinIO, Cloudflare R2, Backblaze B2, and Hetzner Object Storage — all
// speak the S3 protocol. A separate Filesystem implementation covers
// self-hosters who don't want any object-storage service at all.
//
// Keys are opaque; implementations may translate to filesystem paths, S3
// object keys, etc. Callers should not assume a particular separator.
type Store interface {
	// Get returns the object's body. The caller must close the reader.
	// Returns ErrNotFound when the key doesn't exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Put writes the object. Existing objects at the same key are overwritten.
	// contentType may be empty.
	Put(ctx context.Context, key string, body io.Reader, contentType string) error

	// PutPublic writes an object meant to be served publicly (avatars, org
	// logos) and returns a stable, browser-loadable URL for it. The S3 backend
	// sets a public-read ACL + long cache and returns the object URL; the
	// filesystem backend writes the object and returns a URL under the
	// configured public base (served by the backend's /public route).
	PutPublic(ctx context.Context, key string, body io.Reader, contentType string) (string, error)

	// Delete removes the object. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error

	// Has reports whether the key has an object. (Not named Exists because the
	// legacy S3 Client.Exists already takes a bucket argument.)
	Has(ctx context.Context, key string) (bool, error)

	// PresignedGetURL returns a time-limited URL for a third party (typically
	// the browser) to fetch the object directly. Implementations that can't
	// produce one (filesystem, in particular) should return ErrUnsupported.
	PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)

	// Name returns the implementation identifier for admin UI / audit logs.
	Name() string
}
