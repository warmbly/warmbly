package diagnosticauth

import (
	"bytes"
	"context"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-msgauth/dkim"
	"github.com/warmbly/warmbly/internal/models"
)

const MaxMIMEBytes = 1 << 20
const MaxSignatures = 4

func Verify(ctx context.Context, raw []byte, grant models.DiagnosticAuthGrant, lookup func(context.Context, string) ([]string, error)) models.DiagnosticDKIMResult {
	result := models.DiagnosticDKIMResult{DKIM: "unknown", Alignment: "unknown", Verifier: models.DiagnosticDKIMVerifier, ObservedAt: time.Now().UTC()}
	if len(raw) > MaxMIMEBytes || ctx.Err() != nil {
		return result
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return result
	}
	for _, field := range []string{"From", "To", "Message-Id"} {
		if len(msg.Header[field]) != 1 {
			return result
		}
	}
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil || !strings.EqualFold(from.Address, grant.From) {
		return result
	}
	to, err := mail.ParseAddress(msg.Header.Get("To"))
	if err != nil || !strings.EqualFold(to.Address, grant.To) {
		return result
	}
	if strings.Trim(msg.Header.Get("Message-Id"), "<> \t") != strings.Trim(grant.MessageID, "<> \t") {
		return result
	}
	count := len(msg.Header["Dkim-Signature"])
	if count == 0 || count > MaxSignatures {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if lookup == nil {
		lookup = net.DefaultResolver.LookupTXT
	}
	dnsFailure := false
	verified, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{MaxVerifications: MaxSignatures, LookupTXT: func(name string) ([]string, error) {
		rows, err := lookup(ctx, name)
		if err != nil {
			dnsFailure = true
			return nil, err
		}
		if len(rows) > 16 {
			dnsFailure = true
			return nil, context.DeadlineExceeded
		}
		for _, row := range rows {
			if len(row) > 2048 {
				dnsFailure = true
				return nil, context.DeadlineExceeded
			}
		}
		return rows, nil
	}})
	if err != nil || ctx.Err() != nil {
		return result
	}
	unboundSignature := false
	for _, v := range verified {
		if v.Err != nil {
			continue
		}
		signed := make(map[string]bool)
		for _, h := range v.HeaderKeys {
			signed[strings.ToLower(h)] = true
		}
		if !signed["from"] || !signed["to"] || !signed["message-id"] {
			unboundSignature = true
			continue
		}
		result.DKIM = "pass"
		result.SigningDomain = strings.ToLower(v.Domain)
		if at := strings.LastIndex(from.Address, "@"); at >= 0 && strings.EqualFold(from.Address[at+1:], v.Domain) {
			result.Alignment = "pass"
			return result
		}
	}
	if result.DKIM != "pass" && !dnsFailure && !unboundSignature {
		result.DKIM = "fail"
	}
	return result
}
