package models

import "math"

// ObservationMetric describes an instrument reading, not real-recipient inbox probability.
type ObservationMetric struct {
	Version          string               `json:"version"`
	Population       string               `json:"population"`
	DenominatorKind  string               `json:"denominator_kind"`
	Unit             string               `json:"unit"`
	Source           string               `json:"source"`
	PolicyVersion    string               `json:"classification_policy"`
	Numerator        int                  `json:"numerator"`
	Denominator      int                  `json:"denominator"`
	Unresolved       int                  `json:"unresolved"`
	WindowDays       int                  `json:"window_days,omitempty"`
	WindowBasis      string               `json:"window_basis"`
	MissingnessBasis string               `json:"missingness_basis"`
	Value            *float64             `json:"value"`
	Interval         *ObservationInterval `json:"wilson_95_independence_interval"`
}

type ObservationInterval struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
}

func NewObservationMetric(population, denominatorKind, unit string, numerator, denominator, unresolved int) *ObservationMetric {
	m := &ObservationMetric{Version: "observed-v2", Population: population, DenominatorKind: denominatorKind, Unit: unit, Source: "mailbox_observation", PolicyVersion: "first_folder_v2_with_legacy_classifications", Numerator: numerator, Denominator: denominator, Unresolved: unresolved}
	m.WindowBasis, m.MissingnessBasis = "report_date_range_utc", "unclassified_observed_receipts"
	if population == "seed_panel" {
		m.WindowBasis, m.MissingnessBasis = "placement_test_classification_window", "pending_missing_and_unclassified_copies"
	}
	if population == "warmup_pool_major_providers" {
		m.WindowBasis, m.MissingnessBasis = "trailing_utc_days", "not_instrumented_in_legacy_window_counters"
	}
	if denominator <= 0 || numerator < 0 || numerator > denominator || unresolved < 0 {
		return m
	}
	scale := 1.0
	if unit == "percent" {
		scale = 100
	}
	p, n, z := float64(numerator)/float64(denominator), float64(denominator), 1.959963984540054
	v := p * scale
	m.Value = &v
	center := (p + z*z/(2*n)) / (1 + z*z/n)
	half := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / (1 + z*z/n)
	m.Interval = &ObservationInterval{Lower: math.Max(0, center-half) * scale, Upper: math.Min(1, center+half) * scale}
	return m
}
