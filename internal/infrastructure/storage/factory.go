package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// NewFromEnv constructs the active blob store from environment variables.
//
//	BLOB_PROVIDER=s3          -> existing S3 client (works for AWS / MinIO /
//	                             R2 / B2 / Hetzner Object Storage)
//	BLOB_PROVIDER=filesystem  -> NewFilesystem at BLOB_FS_ROOT
//	(unset)                   -> defaults to "s3" for backwards compatibility
//
// awscfg + defaultBucket are only consulted when the S3 provider is selected.
// For non-AWS S3-compatible endpoints, the operator sets standard AWS env vars:
//
//	AWS_ENDPOINT_URL_S3       -> override endpoint (MinIO, R2, etc.)
//	AWS_REGION                -> required by SDK; arbitrary value for MinIO
//	AWS_ACCESS_KEY_ID         -> S3 credential
//	AWS_SECRET_ACCESS_KEY     -> S3 credential
//
// BLOB_BUCKET overrides defaultBucket when set.
func NewFromEnv(ctx context.Context, awscfg aws.Config, defaultBucket string) (Store, error) {
	provider := os.Getenv("BLOB_PROVIDER")
	if provider == "" {
		provider = "s3"
	}
	publicBaseURL := os.Getenv("BLOB_PUBLIC_BASE_URL")
	switch provider {
	case "s3":
		bucket := os.Getenv("BLOB_BUCKET")
		if bucket == "" {
			bucket = defaultBucket
		}
		if bucket == "" {
			return nil, fmt.Errorf("storage: s3 provider requires BLOB_BUCKET or default bucket")
		}
		client, err := NewClient(ctx, awscfg, bucket)
		if err != nil {
			return nil, err
		}
		client.PublicBaseURL = publicBaseURL
		return client, nil
	case "filesystem", "fs":
		root := os.Getenv("BLOB_FS_ROOT")
		if root == "" {
			return nil, fmt.Errorf("storage: filesystem provider requires BLOB_FS_ROOT")
		}
		return NewFilesystem(root, publicBaseURL)
	default:
		return nil, fmt.Errorf("storage: unknown BLOB_PROVIDER %q (want: s3, filesystem)", provider)
	}
}

// Compile-time interface checks.
var (
	_ Store = (*Client)(nil)
	_ Store = (*FilesystemStore)(nil)
)
