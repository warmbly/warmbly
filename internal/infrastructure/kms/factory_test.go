package kms

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestFromEnv_UnknownProvider(t *testing.T) {
	t.Setenv("KMS_PROVIDER", "no-such-provider")
	_, err := FromEnv(context.Background(), aws.Config{}, "fallback")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestFromEnv_LocalRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KMS_PROVIDER", "local")
	t.Setenv("KMS_LOCAL_MASTER_KEY", base64.StdEncoding.EncodeToString(key))

	p, err := FromEnv(context.Background(), aws.Config{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "local" {
		t.Fatalf("expected local, got %q", p.Name())
	}

	plain, ct, err := p.GenerateDataKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.GetDecryptedKey(context.Background(), ct)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatal("round trip mismatch")
	}
}

func TestFromEnv_LocalMissingKeyErrors(t *testing.T) {
	t.Setenv("KMS_PROVIDER", "local")
	t.Setenv("KMS_LOCAL_MASTER_KEY", "")
	t.Setenv("KMS_LOCAL_MASTER_KEY_FILE", "")

	if _, err := FromEnv(context.Background(), aws.Config{}, ""); err == nil {
		t.Fatal("expected error when local key unset")
	}
}

func TestFromEnv_AWSFallsBackToProvidedKeyID(t *testing.T) {
	// AWS provider construction is allowed even without real AWS — it just
	// builds the client. Failure would only occur on real KMS calls.
	t.Setenv("KMS_PROVIDER", "aws")
	t.Setenv("KMS_AWS_KEY_ID", "")
	p, err := FromEnv(context.Background(), aws.Config{}, "alias/fallback")
	if err != nil {
		t.Fatalf("aws provider with fallback key should construct: %v", err)
	}
	if p.Name() != "aws-kms" {
		t.Fatalf("expected aws-kms, got %q", p.Name())
	}
}

func TestFromEnv_AWSWithoutAnyKeyIDErrors(t *testing.T) {
	t.Setenv("KMS_PROVIDER", "aws")
	t.Setenv("KMS_AWS_KEY_ID", "")
	if _, err := FromEnv(context.Background(), aws.Config{}, ""); err == nil {
		t.Fatal("aws provider without key id should error")
	}
}

func TestFromEnv_DefaultsToAWS(t *testing.T) {
	t.Setenv("KMS_PROVIDER", "")
	p, err := FromEnv(context.Background(), aws.Config{}, "alias/x")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "aws-kms" {
		t.Fatalf("unset KMS_PROVIDER should default to aws-kms, got %q", p.Name())
	}
}
