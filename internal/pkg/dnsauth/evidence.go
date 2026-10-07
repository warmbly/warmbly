package dnsauth

import "encoding/json"

type ReadinessComponent struct {
	Component string `json:"component"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
}

func (r Result) Readiness() []ReadinessComponent {
	state := func(found bool) string {
		if r.Domain == "" || r.Reserved || r.LookupError {
			return "unknown"
		}
		if found {
			return "record-present"
		}
		return "record-not-found"
	}
	dkim := state(r.DKIMFound)
	if !r.DKIMFound {
		dkim = "unknown"
	}
	return []ReadinessComponent{
		{Component: "spf_dns", State: state(r.SPFFound), Reason: "DNS discovery does not evaluate the actual sending route."},
		{Component: "dkim_dns", State: dkim, Reason: "Only known selectors are probed; an undiscovered key is not proof of absence."},
		{Component: "dmarc_dns", State: state(r.DMARCFound), Reason: "A published policy, including p=none, does not prove message alignment or inbox placement."},
		{Component: "receiver_authentication", State: "unknown", Reason: "A receiver-attested message evaluation is required for observed aligned pass."},
		{Component: "transport_tls", State: "unknown", Reason: "DNS discovery does not observe delivery transport."},
		{Component: "bulk_one_click_unsubscribe", State: "not-evaluated", Reason: "Applies to eligible marketing/bulk traffic; header presence alone does not establish signed coverage or endpoint behavior."},
	}
}

func (r Result) MarshalJSON() ([]byte, error) {
	type legacy Result
	return json.Marshal(struct {
		legacy
		EvidenceScope          string               `json:"evidence_scope"`
		ObservedAuthentication string               `json:"observed_authentication"`
		Readiness              []ReadinessComponent `json:"readiness"`
	}{legacy: legacy(r), EvidenceScope: "dns_discovery", ObservedAuthentication: "unknown", Readiness: r.Readiness()})
}
