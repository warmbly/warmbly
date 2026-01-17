package dynamo

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type Client struct {
	*dynamodb.Client
}

func NewClient(ctx context.Context, cfg aws.Config) (*Client, error) {
	return &Client{dynamodb.NewFromConfig(cfg)}, nil
}
