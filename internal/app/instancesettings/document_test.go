package instancesettings

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeliverabilityDefaults(t *testing.T) {
	d := Defaults().Deliverability
	if !d.EnforceDomainAuth {
		t.Error("EnforceDomainAuth = false, want true by default")
	}
	if d.AuthGraceHours != AuthGraceHoursDefault {
		t.Errorf("AuthGraceHours = %d, want %d", d.AuthGraceHours, AuthGraceHoursDefault)
	}
	if got, want := d.AuthGrace(), time.Duration(AuthGraceHoursDefault)*time.Hour; got != want {
		t.Errorf("AuthGrace() = %v, want %v", got, want)
	}
}

func TestDeliverabilityNormalize(t *testing.T) {
	tests := []struct {
		name  string
		hours int
		want  int
	}{
		// Zero is a document written before this section existed, or one
		// hand-edited. It must resolve to the default, never to "no grace".
		{"zero resolves to the default", 0, AuthGraceHoursDefault},
		{"negative resolves to the default", -5, AuthGraceHoursDefault},
		{"below the floor clamps up", 0, AuthGraceHoursDefault},
		{"in range is kept", 24, 24},
		{"at the ceiling is kept", AuthGraceHoursMax, AuthGraceHoursMax},
		{"above the ceiling clamps down", AuthGraceHoursMax + 1000, AuthGraceHoursMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Deliverability{AuthGraceHours: tt.hours}
			d.Normalize()
			if d.AuthGraceHours != tt.want {
				t.Errorf("AuthGraceHours = %d, want %d", d.AuthGraceHours, tt.want)
			}
		})
	}
}

// A document stored before the deliverability section existed must come back
// with the section's defaults rather than its zero value, or every existing
// install would silently read "enforcement off, zero grace".
func TestDocumentUnmarshalOverDefaultsKeepsDeliverability(t *testing.T) {
	doc := Defaults()
	stored := []byte(`{"invitations":{"links_enabled":false,"ttl_hours":24}}`)
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	doc.Normalize()

	if !doc.Deliverability.EnforceDomainAuth {
		t.Error("EnforceDomainAuth = false, want the default true to survive an older document")
	}
	if doc.Deliverability.AuthGraceHours != AuthGraceHoursDefault {
		t.Errorf("AuthGraceHours = %d, want %d", doc.Deliverability.AuthGraceHours, AuthGraceHoursDefault)
	}
	if doc.Invitations.TTLHours != 24 {
		t.Errorf("TTLHours = %d, want the stored 24", doc.Invitations.TTLHours)
	}
}

// An operator turning the gate off is an explicit false, which must survive the
// round trip. A pointer field in the patch is what makes false distinguishable
// from absent.
func TestPatchDeliverability(t *testing.T) {
	off := false
	hours := 12

	var p Patch
	if err := json.Unmarshal([]byte(`{"deliverability":{"enforce_domain_auth":false,"auth_grace_hours":12}}`), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := p.Apply(Defaults())
	if got.Deliverability.EnforceDomainAuth != off {
		t.Error("EnforceDomainAuth = true, want the patched false")
	}
	if got.Deliverability.AuthGraceHours != hours {
		t.Errorf("AuthGraceHours = %d, want %d", got.Deliverability.AuthGraceHours, hours)
	}
}

func TestPatchDeliverabilityAbsentKeepsStored(t *testing.T) {
	stored := Defaults()
	stored.Deliverability = Deliverability{EnforceDomainAuth: false, AuthGraceHours: 5}

	var p Patch
	if err := json.Unmarshal([]byte(`{"access":{"allow_invited_signup":false}}`), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := p.Apply(stored)
	if got.Deliverability.EnforceDomainAuth {
		t.Error("EnforceDomainAuth = true, want the stored false to survive an unrelated patch")
	}
	if got.Deliverability.AuthGraceHours != 5 {
		t.Errorf("AuthGraceHours = %d, want the stored 5", got.Deliverability.AuthGraceHours)
	}
}

// An explicit zero from a client is a mistake, not a request for no grace.
func TestPatchDeliverabilityZeroGraceClampsToFloor(t *testing.T) {
	var p Patch
	if err := json.Unmarshal([]byte(`{"deliverability":{"auth_grace_hours":0}}`), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := p.Apply(Defaults())
	if got.Deliverability.AuthGraceHours != AuthGraceHoursMin {
		t.Errorf("AuthGraceHours = %d, want the floor %d", got.Deliverability.AuthGraceHours, AuthGraceHoursMin)
	}
}
