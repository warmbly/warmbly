package crypt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
)

var ErrPemDecode = errors.New("failed to decode pem block")
var ErrPublicKey = errors.New("not an RSA public key")
var ErrRSAKeySize = errors.New("invalid rsa key size")
var ErrNonceSize = errors.New("ciphertext too short for nonce")

func ValidateRSAPublicKey(pubB64 string) error {
	pemBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return fmt.Errorf("invalid base64: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return errors.New("not valid PEM")
	}

	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("invalid RSA public key: %w", err)
	}

	if _, ok := pubInterface.(*rsa.PublicKey); !ok {
		return errors.New("key is not RSA")
	}

	return nil
}
