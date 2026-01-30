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
	"io"
)

type AsymmetricPublicClient interface {
	Encrypt(message []byte) ([]byte, error)
}

type asymmetricPublicClient struct {
	publicKey *rsa.PublicKey
}

func AsymmetricPublicKey(publicKeyB64 string) (AsymmetricPublicClient, error) {
	pemBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, ErrPemDecode
	}

	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	pubKey, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		return nil, ErrPublicKey
	}

	return &asymmetricPublicClient{
		publicKey: pubKey,
	}, nil
}

// encrypts data using AES-256-GCM + RSA for AES key
func (c *asymmetricPublicClient) Encrypt(message []byte) ([]byte, error) {
	//  Generate random AES key
	aesKey := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return nil, err
	}

	// Encrypt message with AES-GCM
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, message, nil)

	// Encrypt AES key with RSA public key
	encAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, c.publicKey, aesKey, nil)
	if err != nil {
		return nil, err
	}

	// Combine both
	return append(encAESKey, ciphertext...), nil
}
