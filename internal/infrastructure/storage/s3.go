package storage

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	Bucket string
	*s3.Client
}

func NewClient(ctx context.Context, cfg aws.Config, bucket string) (*Client, error) {
	return &Client{
		Bucket: bucket,
		Client: s3.NewFromConfig(cfg),
	}, nil
}
