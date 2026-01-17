package crypt

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
)

func makeKeyPairB64(t *testing.T) (privB64, pubB64 string) {
	t.Helper()

	// Generate a test RSA keypair
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// --- Private key ---
	privBytes := x509.MarshalPKCS1PrivateKey(privKey)
	privPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})
	privB64 = base64.StdEncoding.EncodeToString(privPem)

	// --- Public key ---
	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	pubB64 = base64.StdEncoding.EncodeToString(pubPem)

	return privB64, pubB64
}

func TestAsymmetric_EncryptDecrypt_RoundTrip(t *testing.T) {
	privB64, pubB64 := makeKeyPairB64(t)

	privClient, err := AsymmetricPrivateKey(privB64)
	if err != nil {
		t.Fatalf("AsymmetricPrivateKey failed: %v", err)
	}
	pubClient, err := AsymmetricPublicKey(pubB64)
	if err != nil {
		t.Fatalf("AsymmetricPublicKey failed: %v", err)
	}

	tests := []string{
		"",
		"Hello, world!",
		"🚀 This message should survive RSA+AES encryption!",
	}

	for _, msg := range tests {
		t.Run(msg, func(t *testing.T) {
			ciphertext, err := pubClient.Encrypt([]byte(msg))
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			plaintext, err := privClient.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if !bytes.Equal(plaintext, []byte(msg)) {
				t.Errorf("decrypted mismatch:\n got=%q\nwant=%q", plaintext, msg)
			}
		})
	}
}

func TestAsymmetricPublicKey_InvalidInputs(t *testing.T) {
	// Not base64
	if _, err := AsymmetricPublicKey("invalid!!"); err == nil {
		t.Error("expected error for invalid base64 input")
	}

	// Not a PEM block
	badPem := base64.StdEncoding.EncodeToString([]byte("not pem"))
	if _, err := AsymmetricPublicKey(badPem); err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestAsymmetricPrivateKey_MismatchedKeys(t *testing.T) {
	// Two independent keypairs
	_, pub1B64 := makeKeyPairB64(t)
	priv2B64, _ := makeKeyPairB64(t)

	privClient2, _ := AsymmetricPrivateKey(priv2B64)
	pubClient1, _ := AsymmetricPublicKey(pub1B64)

	ciphertext, err := pubClient1.Encrypt([]byte("top secret"))
	if err != nil {
		t.Fatal(err)
	}

	// Decrypt with wrong key → should fail
	if _, err := privClient2.Decrypt(ciphertext); err == nil {
		t.Error("expected decryption failure with mismatched key")
	}
}
