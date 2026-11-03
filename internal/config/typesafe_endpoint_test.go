package config

import "testing"

func TestTypeSafeEndpoint(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"https://api.typesafe.ai/v1/systemone", true},
		{"  https://eu.typesafe.example/v1/systemone  ", true},
		{"http://localhost:9000/v1/systemone", true},
		{"http://127.0.0.1:9000/v1", true},
		{"http://10.0.0.5/v1", true},
		{"http://api.typesafe.ai/v1/systemone", false},
		{"ftp://api.typesafe.ai/v1", false},
		{"api.typesafe.ai/v1", false},
	} {
		t.Setenv("TYPESAFE_ENDPOINT", tc.value)
		_, err := TypeSafeEndpoint()
		if (err == nil) != tc.ok {
			t.Errorf("TypeSafeEndpoint(%q) err = %v, want ok %v", tc.value, err, tc.ok)
		}
	}
}
