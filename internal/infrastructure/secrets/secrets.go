package secrets

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type SecretsManagerClient struct {
	client *secretsmanager.Client
}

func NewSecretsManagerClient(ctx context.Context, cfg aws.Config) (*SecretsManagerClient, error) {
	return &SecretsManagerClient{
		client: secretsmanager.NewFromConfig(cfg),
	}, nil
}

func (s *SecretsManagerClient) Get(ctx context.Context, secretID string) (string, error) {
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	}
	output, err := s.client.GetSecretValue(ctx, input)
	if err != nil {
		return "", fmt.Errorf("Secrets Manager GetSecretValue failed for %s: %w", secretID, err)
	}
	if output.SecretString == nil {
		return "", fmt.Errorf("secret %s not found or empty", secretID)
	}
	return *output.SecretString, nil
}
