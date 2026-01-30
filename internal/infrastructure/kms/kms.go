package kms

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type KMS struct {
	c         *kms.Client
	MasterKey string
}

func New(ctx context.Context, cfg aws.Config, keyID string) (*KMS, error) {
	return &KMS{
		c:         kms.NewFromConfig(cfg),
		MasterKey: keyID,
	}, nil
}
