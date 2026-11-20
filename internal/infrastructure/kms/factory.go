package kms

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// FromEnv constructs the active KMS provider from environment variables.
//
//	KMS_PROVIDER=aws       -> NewAWS (uses awscfg, KMS_AWS_KEY_ID or fallbackAWSKeyID)
//	KMS_PROVIDER=local     -> NewLocalFromEnv
//	KMS_PROVIDER=brokered  -> node-side: no key material, no cloud credential;
//	                          the control plane opens sealed keys for it
//	(unset)                -> defaults to "aws" for backwards compatibility
//
// Callers pass an AWS config that's only consulted when the AWS provider is
// selected. The fallbackAWSKeyID is used when KMS_AWS_KEY_ID is empty — this
// preserves the historical "alias/master-key[-dev]" default used at boot.
func FromEnv(ctx context.Context, awscfg aws.Config, fallbackAWSKeyID string, brokered ...BrokeredOption) (Provider, error) {
	provider := os.Getenv("KMS_PROVIDER")
	if provider == "" {
		provider = "aws"
	}
	switch provider {
	case "aws", "aws-kms":
		keyID := os.Getenv("KMS_AWS_KEY_ID")
		if keyID == "" {
			keyID = fallbackAWSKeyID
		}
		if keyID == "" {
			return nil, fmt.Errorf("kms: aws provider requires KMS_AWS_KEY_ID or fallback key id")
		}
		return New(ctx, awscfg, keyID)
	case "local":
		return NewLocalFromEnv()
	case "brokered":
		// Same pair every other internal-API client on a node uses.
		baseURL := os.Getenv("ENCRYPTED_KEYS_BACKEND_URL")
		if baseURL == "" {
			baseURL = os.Getenv("WARMBLY_BACKEND_URL")
		}
		// The broker endpoints take their own credential when the instance
		// issues one, so a split deployment can keep this off the edge
		// services. It falls back to the shared internal token.
		token := os.Getenv("NODE_BROKER_TOKEN")
		if token == "" {
			token = os.Getenv("INTERNAL_API_TOKEN")
		}
		if token == "" {
			token = os.Getenv("ENCRYPTED_KEYS_WORKER_TOKEN")
		}
		return NewBrokered(baseURL, token, brokered...)
	default:
		return nil, fmt.Errorf("kms: unknown KMS_PROVIDER %q (want: aws, local, brokered)", provider)
	}
}
