package eventbus

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"github.com/nats-io/nats.go"
)

func natsTLSOption() (nats.Option, error) {
	raw := strings.TrimSpace(os.Getenv("NATS_TLS_CA_B64"))
	if raw == "" {
		return nil, nil
	}
	pem, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("eventbus nats: NATS_TLS_CA_B64 must contain base64 PEM certificates")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("eventbus nats: load system certificate authorities")
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("eventbus nats: NATS_TLS_CA_B64 contains no certificates")
	}
	return nats.Secure(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}), nil
}
