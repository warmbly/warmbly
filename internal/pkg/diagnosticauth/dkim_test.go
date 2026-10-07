package diagnosticauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"
	"github.com/warmbly/warmbly/internal/models"
)

func TestSignedDiagnosticProofIsBoundedAndContextBound(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	dns := func(context.Context, string) ([]string, error) {
		return []string{"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pub)}, nil
	}
	grant := models.DiagnosticAuthGrant{MessageID: "probe@example.test", From: "sender@example.test", To: "recipient@example.test"}
	plain := "From: sender@example.test\r\nTo: recipient@example.test\r\nMessage-ID: <probe@example.test>\r\nSubject: Disclosed diagnostic\r\n\r\nDiagnostic fixture only.\r\n"
	var signed bytes.Buffer
	if err = dkim.Sign(&signed, strings.NewReader(plain), &dkim.SignOptions{Domain: "example.test", Selector: "diagnostic", Signer: key, HeaderKeys: []string{"From", "To", "Message-ID", "Subject"}}); err != nil {
		t.Fatal(err)
	}
	raw := signed.Bytes()
	for _, tt := range []struct {
		name        string
		raw         []byte
		grant       models.DiagnosticAuthGrant
		dkim, align string
	}{
		{"signed", raw, grant, "pass", "pass"},
		{"body mutation", bytes.Replace(raw, []byte("fixture only"), []byte("forged claim"), 1), grant, "fail", "unknown"},
		{"unverified copied headers", []byte("Authentication-Results: receiver.test; dkim=pass; spf=pass; dmarc=pass\r\n" + plain), grant, "unknown", "unknown"},
		{"wrong exact parent", raw, models.DiagnosticAuthGrant{MessageID: "unrelated@example.test", From: grant.From, To: grant.To}, "unknown", "unknown"},
		{"wrong receiver", raw, models.DiagnosticAuthGrant{MessageID: grant.MessageID, From: grant.From, To: "other@example.test"}, "unknown", "unknown"},
		{"oversize", bytes.Repeat([]byte("x"), MaxMIMEBytes+1), grant, "unknown", "unknown"},
		{"signature cap", []byte(strings.Repeat("DKIM-Signature: v=1; d=example.test; s=diagnostic; b=bad\r\n", MaxSignatures) + string(raw)), grant, "unknown", "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := Verify(t.Context(), tt.raw, tt.grant, dns)
			if result.DKIM != tt.dkim || result.Alignment != tt.align || !result.Valid(time.Now()) {
				t.Fatalf("evidence=%+v", result)
			}
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	result := Verify(ctx, raw, grant, func(ctx context.Context, _ string) ([]string, error) { <-ctx.Done(); return nil, ctx.Err() })
	if result.DKIM != "unknown" || result.Alignment != "unknown" {
		t.Fatal("DNS timeout upgraded unknown", result)
	}
	var unaligned bytes.Buffer
	if err = dkim.Sign(&unaligned, strings.NewReader(plain), &dkim.SignOptions{Domain: "provider.test", Selector: "diagnostic", Signer: key, HeaderKeys: []string{"From", "To", "Message-ID"}}); err != nil {
		t.Fatal(err)
	}
	result = Verify(t.Context(), unaligned.Bytes(), grant, dns)
	if result.DKIM != "pass" || result.Alignment != "unknown" {
		t.Fatal("unaligned signature became alignment proof", result)
	}
	var unbound bytes.Buffer
	if err = dkim.Sign(&unbound, strings.NewReader(plain), &dkim.SignOptions{Domain: "example.test", Selector: "diagnostic", Signer: key, HeaderKeys: []string{"From"}}); err != nil {
		t.Fatal(err)
	}
	result = Verify(t.Context(), unbound.Bytes(), grant, dns)
	if result.DKIM != "unknown" || result.Alignment != "unknown" {
		t.Fatal("unbound valid signature became diagnostic pass/fail", result)
	}
}
