package confenge

import (
	"strings"
	"time"
)

// OperatorRejection captures one-click rejection reasons for doctrine improvement.
type OperatorRejection struct {
	Reason          string    `json:"reason"`
	DraftID         string    `json:"draft_id,omitempty"`
	DoctrineVersion string    `json:"doctrine_version"`
	ServiceCode     string    `json:"service_code,omitempty"`
	OfferCode       string    `json:"offer_code,omitempty"`
	At              time.Time `json:"at"`
}

// OperatorEditSignal summarizes human edits before approval (accumulate only; no auto-train).
type OperatorEditSignal struct {
	Codes           []string  `json:"codes"`
	DraftID         string    `json:"draft_id,omitempty"`
	DoctrineVersion string    `json:"doctrine_version"`
	OriginalLen     int       `json:"original_len"`
	EditedLen       int       `json:"edited_len"`
	At              time.Time `json:"at"`
}

// ValidRejectionReason checks against doctrine copy_rules.
func ValidRejectionReason(pb *Playbook, reason string) bool {
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		return false
	}
	if pb == nil {
		pb, _ = LoadPlaybook()
	}
	if pb == nil {
		return reason == "other"
	}
	for _, r := range pb.CopyRules.RejectionReasons {
		if strings.EqualFold(r, reason) {
			return true
		}
	}
	return false
}

// ClassifyEditSignals infers coarse edit codes from original vs edited body (heuristic).
func ClassifyEditSignals(original, edited string) []string {
	o, e := strings.TrimSpace(original), strings.TrimSpace(edited)
	if o == e {
		return nil
	}
	var codes []string
	if countWords(e) < countWords(o) {
		codes = append(codes, "shortened")
	}
	ol, el := strings.ToLower(o), strings.ToLower(e)
	// Softened: removed hard claim markers
	for _, hard := range []string{"têm r$", "tem r$", "é certo", "garantimos", "a receber"} {
		if strings.Contains(ol, hard) && !strings.Contains(el, hard) {
			codes = append(codes, "softened_claim")
			break
		}
	}
	if strings.Count(o, "?") != strings.Count(e, "?") ||
		(strings.Contains(ol, "checklist") != strings.Contains(el, "checklist")) {
		codes = append(codes, "changed_cta")
	}
	for _, j := range []string{"sinergia", "potencializar", "solução 360", "revolucion"} {
		if strings.Contains(ol, j) && !strings.Contains(el, j) {
			codes = append(codes, "removed_jargon")
			break
		}
	}
	if len(codes) == 0 {
		codes = append(codes, "other")
	}
	return codes
}

// NewOperatorRejection builds a rejection learning record.
func NewOperatorRejection(reason, draftID, service, offer string) OperatorRejection {
	return OperatorRejection{
		Reason:          reason,
		DraftID:         draftID,
		DoctrineVersion: OutreachDoctrineVersion,
		ServiceCode:     service,
		OfferCode:       offer,
		At:              time.Now().UTC(),
	}
}

// NewOperatorEditSignal builds an edit learning record.
func NewOperatorEditSignal(draftID, original, edited string) OperatorEditSignal {
	return OperatorEditSignal{
		Codes:           ClassifyEditSignals(original, edited),
		DraftID:         draftID,
		DoctrineVersion: OutreachDoctrineVersion,
		OriginalLen:     len([]rune(original)),
		EditedLen:       len([]rune(edited)),
		At:              time.Now().UTC(),
	}
}
