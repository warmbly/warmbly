package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
)

type AsymmetricPrivateClient interface {
	Decrypt(body []byte) ([]byte, error)
}

type asymmetricPrivateClient struct {
	secretKey *rsa.PrivateKey
}

func AsymmetricPrivateKey(privateKeyB64 string) (AsymmetricPrivateClient, error) {
	pemBytes, err := base64.StdEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, ErrPemDecode
	}

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return &asymmetricPrivateClient{
		secretKey: privKey,
	}, nil
}

// decrypts data encrypted with asymmetricPublicClient
func (c *asymmetricPrivateClient) Decrypt(body []byte) ([]byte, error) {
	// Extract RSA-encrypted AES key size
	rsaKeySize := c.secretKey.Size()
	if len(body) < rsaKeySize {
		return nil, ErrRSAKeySize
	}
	encAESKey := body[:rsaKeySize]
	encryptedMessage := body[rsaKeySize:]

	// Decrypt AES key
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, c.secretKey, encAESKey, nil)
	if err != nil {
		return nil, err
	}

	// Decrypt message with AES-GCM
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(encryptedMessage) < nonceSize {
		return nil, err
	}

	nonce, ciphertext := encryptedMessage[:nonceSize], encryptedMessage[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
