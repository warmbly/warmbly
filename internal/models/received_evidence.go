package models

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const receivedEvidenceFlag = "Warmbly-Observed-Evidence:"

const ObservationUnknownFolderFlag = "Warmbly-Unknown-Folder"

// ReceivedEvidence separates header claims from verified receiver verdicts.
type ReceivedEvidence struct {
	DKIMVerification             *DiagnosticDKIMResult `json:"dkim_verification,omitempty"`
	Version                      string                `json:"version"`
	Source                       string                `json:"source"`
	Trust                        string                `json:"trust"`
	ObservedAt                   time.Time             `json:"observed_at"`
	SPF                          string                `json:"spf"`
	DKIM                         string                `json:"dkim"`
	DMARC                        string                `json:"dmarc"`
	Alignment                    string                `json:"alignment"`
	TLS                          string                `json:"tls"`
	FromDomain                   string                `json:"from_domain,omitempty"`
	EnvelopeDomain               string                `json:"envelope_domain_claim,omitempty"`
	SigningDomain                string                `json:"signing_domain_claim,omitempty"`
	Selector                     string                `json:"selector_claim,omitempty"`
	AuthenticationResultsPresent bool                  `json:"authentication_results_present"`
	OneClickHeaders              bool                  `json:"one_click_headers_present"`
	OneClickSigningClaim         bool                  `json:"one_click_signing_claim"`
	OneClickCompliance           string                `json:"one_click_compliance"`
}

func UnknownReceivedEvidence() *ReceivedEvidence {
	return &ReceivedEvidence{Version: "received-v1", Source: "unavailable", Trust: "unknown", SPF: "unknown", DKIM: "unknown", DMARC: "unknown", Alignment: "unknown", TLS: "unknown", OneClickCompliance: "unknown"}
}

func (e *ReceivedEvidence) Flag() string {
	b, _ := json.Marshal(e)
	return receivedEvidenceFlag + base64.RawURLEncoding.EncodeToString(b)
}

func WithReceivedEvidence(flags []string, e *ReceivedEvidence) []string {
	out := make([]string, 0, len(flags)+1)
	for _, flag := range flags {
		if !strings.HasPrefix(flag, receivedEvidenceFlag) {
			out = append(out, flag)
		}
	}
	return append(out, e.Flag())
}

func PlacementObservationFlags(flags []string) []string {
	out := make([]string, 0)
	for _, flag := range flags {
		if len(flag) <= 128 && !strings.ContainsAny(flag, ":\r\n") && len(out) < 64 {
			out = append(out, flag)
		}
	}
	return out
}

// EvidenceFromFlags never upgrades header metadata into trusted authentication.
func EvidenceFromFlags(flags []string) *ReceivedEvidence {
	for i := len(flags) - 1; i >= 0; i-- {
		flag := flags[i]
		if !strings.HasPrefix(flag, receivedEvidenceFlag) || len(flag) > 4096 {
			continue
		}
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(flag, receivedEvidenceFlag))
		if err != nil {
			continue
		}
		e := UnknownReceivedEvidence()
		if json.Unmarshal(b, e) != nil || e.Version != "received-v1" {
			continue
		}
		e.Trust, e.SPF, e.DKIM, e.DMARC, e.Alignment, e.TLS, e.OneClickCompliance = "unverified_headers", "unknown", "unknown", "unknown", "unknown", "unknown", "unknown"
		e.DKIMVerification = nil
		return e
	}
	return UnknownReceivedEvidence()
}
