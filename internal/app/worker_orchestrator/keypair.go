package worker_orchestrator

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// GenerateKeypair produces an ed25519 SSH keypair.
// Returns:
//   - publicKey  in OpenSSH authorized_keys format ("ssh-ed25519 AAAA... warmbly")
//   - privateKey in OpenSSH PEM format (PEM-encoded openssh-key-v1)
func GenerateKeypair() (publicKey string, privateKeyPEM string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("ed25519: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("ssh.NewPublicKey: %w", err)
	}
	authorizedKey := ssh.MarshalAuthorizedKey(sshPub)
	publicKey = string(authorizedKey[:len(authorizedKey)-1]) + " warmbly-worker"

	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", fmt.Errorf("ssh.MarshalPrivateKey: %w", err)
	}
	privateKeyPEM = string(pem.EncodeToMemory(pemBlock))

	return publicKey, privateKeyPEM, nil
}

// FingerprintSHA256 returns the SHA256 fingerprint of a host key, formatted
// the way OpenSSH displays it: "SHA256:<base64-no-padding>".
func FingerprintSHA256(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// ParsePrivateKey decodes an OpenSSH PEM private key into an ssh.Signer.
func ParsePrivateKey(pemBytes []byte) (ssh.Signer, error) {
	if len(pemBytes) == 0 {
		return nil, errors.New("empty private key")
	}
	return ssh.ParsePrivateKey(pemBytes)
}
