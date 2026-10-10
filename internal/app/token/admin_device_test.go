package token

import (
	"testing"
	"time"
)

func TestAdminDeviceProofCannotCreateFreshnessOrMFA(t *testing.T) {
	now := time.Now().UTC()
	proof := now.Add(-time.Minute)
	expires := now.Add(time.Hour)
	revoked := now.Add(-time.Second)
	older := now.Add(-6 * time.Minute)
	future := now.Add(time.Minute)
	tests := []struct {
		name                     string
		mfa                      bool
		revoked, expires, reauth *time.Time
		approved                 time.Time
		want                     bool
	}{
		{"original recent proof", true, nil, &expires, &proof, proof, true},
		{"missing MFA", false, nil, &expires, &proof, proof, false},
		{"revoked source", true, &revoked, &expires, &proof, proof, false},
		{"expired source", true, nil, &older, &proof, proof, false},
		{"missing source proof", true, nil, &expires, nil, proof, false},
		{"invented newer proof", true, nil, &expires, &older, proof, false},
		{"old approval stays old after source reauth", true, nil, &expires, &proof, older, false},
		{"future proof", true, nil, &expires, &future, future, false},
		{"future source cannot bless older delegated proof", true, nil, &expires, &future, proof, false},
		{"later source reauth keeps original proof", true, nil, &expires, &now, proof, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validAdminDeviceProof(tc.mfa, tc.revoked, tc.expires, tc.reauth, tc.approved, now); got != tc.want {
				t.Fatalf("proof accepted=%v, want=%v", got, tc.want)
			}
		})
	}
}
