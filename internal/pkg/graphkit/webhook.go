package graphkit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader carries the webhook signature.
const SignatureHeader = "GraphKit-Signature"

// SignatureTolerance is how far a signature's timestamp may be from now.
const SignatureTolerance = 5 * time.Minute

var (
	// ErrNoSecret means no webhook secret is configured, so nothing can verify.
	ErrNoSecret = errors.New("graphkit: no webhook secret configured")
	// ErrSignatureMissing is an absent or malformed signature header.
	ErrSignatureMissing = errors.New("graphkit: signature missing or malformed")
	// ErrSignatureExpired is a timestamp outside the tolerance.
	ErrSignatureExpired = errors.New("graphkit: signature timestamp outside tolerance")
	// ErrSignatureMismatch is a signature no configured secret produced.
	ErrSignatureMismatch = errors.New("graphkit: signature mismatch")
)

// Sign is the header value GraphKit sends for body at t under secret.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac(secret, ts, body))
}

func mac(secret, ts string, body []byte) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(body)
	return m.Sum(nil)
}

// VerifySignature checks header against body: t within SignatureTolerance of
// now, and any v1 equal (in constant time) to the HMAC under any non-empty
// secret, so the previous secret keeps verifying through a rotation.
func VerifySignature(header string, body []byte, secrets []string, now time.Time) error {
	var keys []string
	for _, s := range secrets {
		if s = strings.TrimSpace(s); s != "" {
			keys = append(keys, s)
		}
	}
	if len(keys) == 0 {
		return ErrNoSecret
	}
	var ts string
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			if b, err := hex.DecodeString(v); err == nil && len(b) == sha256.Size {
				sigs = append(sigs, b)
			}
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrSignatureMissing
	}
	if d := now.Sub(time.Unix(sec, 0)); d > SignatureTolerance || d < -SignatureTolerance {
		return ErrSignatureExpired
	}
	ok := 0
	for _, k := range keys {
		want := mac(k, ts, body)
		for _, s := range sigs {
			// Every comparison runs, so timing says nothing about which matched.
			ok |= boolInt(hmac.Equal(want, s))
		}
	}
	if ok == 0 {
		return ErrSignatureMismatch
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
