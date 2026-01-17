package ssm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type SSMParameterStore struct {
	client *ssm.Client
}

func NewSSMParameterStore(ctx context.Context, cfg aws.Config) (*SSMParameterStore, error) {
	return &SSMParameterStore{
		client: ssm.NewFromConfig(cfg),
	}, nil
}

func (s *SSMParameterStore) Get(ctx context.Context, name string) (string, error) {
	input := &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true), // auto-decrypt SecureString
	}
	output, err := s.client.GetParameter(ctx, input)
	if err != nil {
		return "", fmt.Errorf("SSM GetParameter failed for %s: %w", name, err)
	}
	if output.Parameter == nil || output.Parameter.Value == nil {
		return "", fmt.Errorf("parameter %s not found or empty", name)
	}
	return *output.Parameter.Value, nil
}
