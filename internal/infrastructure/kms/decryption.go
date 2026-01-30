package kms

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type Decryption struct {
	plainDEK []byte
}

func (k *KMS) GetDecryptedKey(ctx context.Context, encDEKB64 string) ([]byte, error) {
	encDEK, err := base64.StdEncoding.DecodeString(encDEKB64)
	if err != nil {
		return nil, err
	}

	// Decrypt DEK with KMS
	dkOutput, err := k.c.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: encDEK,
	})
	if err != nil {
		return nil, err
	}

	return dkOutput.Plaintext, nil
}
