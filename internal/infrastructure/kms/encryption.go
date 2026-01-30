package kms

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

func (k *KMS) GenerateDataKey(ctx context.Context) ([]byte, string, error) {
	dkOutput, err := k.c.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:         aws.String(k.MasterKey),
		NumberOfBytes: aws.Int32(32), // AES-256
	})
	if err != nil {
		return nil, "", err
	}

	return dkOutput.Plaintext, base64.StdEncoding.EncodeToString(dkOutput.CiphertextBlob), nil
}
