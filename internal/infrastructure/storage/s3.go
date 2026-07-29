package storage

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	Bucket string
	// PublicBaseURL, when set, overrides the default public URL used by
	// PutPublic (needed for MinIO / R2 / self-hosted S3 that don't serve at
	// s3.amazonaws.com). Empty falls back to the AWS virtual-hosted URL.
	PublicBaseURL string
	*s3.Client
}

func NewClient(ctx context.Context, cfg aws.Config, bucket string) (*Client, error) {
	return &Client{
		Bucket: bucket,
		Client: s3.NewFromConfig(cfg, func(o *s3.Options) {
			// Custom endpoints (LocalStack, MinIO) need path-style requests:
			// virtual-hosted addressing puts the bucket in the hostname
			// (bucket.localhost:4566), which these servers don't resolve as a
			// bucket. Real AWS (no endpoint override) keeps the default.
			if os.Getenv("AWS_ENDPOINT_URL") != "" || os.Getenv("AWS_ENDPOINT_URL_S3") != "" {
				o.UsePathStyle = true
			}
		}),
	}, nil
}
