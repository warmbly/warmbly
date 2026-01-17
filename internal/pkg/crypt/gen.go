package crypt

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
)

func Nonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func VerificationCode() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RID(length int) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	for i := range length {
		b[i] = charset[int(b[i])%len(charset)]
	}

	return string(b), nil
}
