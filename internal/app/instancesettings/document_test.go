package instancesettings

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/config"
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

func TestTrackingDefaults(t *testing.T) {
	tr := Defaults().Tracking
	if tr.MachineWindowOpenSeconds != config.TrackingMachineWindowOpenSecondsDefault {
		t.Errorf("MachineWindowOpenSeconds = %d, want %d", tr.MachineWindowOpenSeconds, config.TrackingMachineWindowOpenSecondsDefault)
	}
	if tr.MachineWindowClickSeconds != config.TrackingMachineWindowClickSecondsDefault {
		t.Errorf("MachineWindowClickSeconds = %d, want %d", tr.MachineWindowClickSeconds, config.TrackingMachineWindowClickSecondsDefault)
	}
	if tr.MachineWindowProbableSeconds != config.TrackingMachineWindowProbableSecondsDefault {
		t.Errorf("MachineWindowProbableSeconds = %d, want %d", tr.MachineWindowProbableSeconds, config.TrackingMachineWindowProbableSecondsDefault)
	}
	// The click window is the tighter of the two on purpose: a misjudged
	// click costs an automation, a misjudged open costs a metric.
	if tr.ClickWindow() > tr.OpenWindow() {
		t.Errorf("click window %v must not exceed the open window %v", tr.ClickWindow(), tr.OpenWindow())
	}
	// The probable window is the widest, because the label it goes with only
	// moves the odds. Shipping it narrower than either would make naming a
	// network pointless, and the floor below would silently hide that.
	for _, kind := range []time.Duration{tr.OpenWindow(), tr.ClickWindow()} {
		if tr.ProbableWindow(kind) < kind {
			t.Errorf("probable window %v is under the %v it must never weaken", tr.ProbableWindow(kind), kind)
		}
		if tr.ProbableWindow(kind) <= kind {
			t.Errorf("probable window %v buys nothing over %v", tr.ProbableWindow(kind), kind)
		}
	}
}

// The floor is the one-way guarantee the catalogue rests on: naming a network
// probable may only ever catch more scans, so an operator who sets the window
// below a per-kind one cannot hand a scan back.
func TestProbableWindowNeverFallsBelowTheKindWindow(t *testing.T) {
	tr := Tracking{MachineWindowOpenSeconds: 300, MachineWindowClickSeconds: 200, MachineWindowProbableSeconds: 1}
	tr.Normalize()
	for _, kind := range []time.Duration{tr.OpenWindow(), tr.ClickWindow()} {
		if got := tr.ProbableWindow(kind); got != kind {
			t.Errorf("ProbableWindow(%v) = %v, want the kind window itself", kind, got)
		}
	}
	wide := Tracking{MachineWindowProbableSeconds: 7200}
	wide.Normalize()
	if got, want := wide.ProbableWindow(time.Minute), 2*time.Hour; got != want {
		t.Errorf("ProbableWindow = %v, want the wider %v", got, want)
	}
}

func TestTrackingNormalize(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    int
	}{
		// Zero is a document written before this section existed. It must
		// resolve to the default, never to "nothing is ever automated".
		{"zero resolves to the default", 0, config.TrackingMachineWindowOpenSecondsDefault},
		{"negative resolves to the default", -30, config.TrackingMachineWindowOpenSecondsDefault},
		{"in range is kept", 90, 90},
		{"at the floor is kept", config.TrackingMachineWindowSecondsMin, config.TrackingMachineWindowSecondsMin},
		{"at the ceiling is kept", config.TrackingMachineWindowSecondsMax, config.TrackingMachineWindowSecondsMax},
		{"above the ceiling clamps down", config.TrackingMachineWindowSecondsMax + 600, config.TrackingMachineWindowSecondsMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := Tracking{MachineWindowOpenSeconds: tt.seconds}
			tr.Normalize()
			if tr.MachineWindowOpenSeconds != tt.want {
				t.Errorf("MachineWindowOpenSeconds = %d, want %d", tr.MachineWindowOpenSeconds, tt.want)
			}
		})
	}
}

// A document stored before the tracking section existed must come back with
// the shipped windows rather than zero, which would read as "never automated"
// and let every delivery-time scan count as a person.
func TestDocumentUnmarshalOverDefaultsKeepsTracking(t *testing.T) {
	doc := Defaults()
	stored := []byte(`{"invitations":{"links_enabled":false,"ttl_hours":24}}`)
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	doc.Normalize()

	if doc.Tracking.MachineWindowOpenSeconds != config.TrackingMachineWindowOpenSecondsDefault {
		t.Errorf("MachineWindowOpenSeconds = %d, want the default %d to survive an older document",
			doc.Tracking.MachineWindowOpenSeconds, config.TrackingMachineWindowOpenSecondsDefault)
	}
	if doc.Tracking.MachineWindowClickSeconds != config.TrackingMachineWindowClickSecondsDefault {
		t.Errorf("MachineWindowClickSeconds = %d, want the default %d to survive an older document",
			doc.Tracking.MachineWindowClickSeconds, config.TrackingMachineWindowClickSecondsDefault)
	}
	if doc.Tracking.MachineWindowProbableSeconds != config.TrackingMachineWindowProbableSecondsDefault {
		t.Errorf("MachineWindowProbableSeconds = %d, want the default %d to survive an older document",
			doc.Tracking.MachineWindowProbableSeconds, config.TrackingMachineWindowProbableSecondsDefault)
	}
}

func TestPatchTracking(t *testing.T) {
	doc := Defaults()
	open, click, probable := 120, 45, 1800
	patch := Patch{Tracking: &struct {
		MachineWindowOpenSeconds     *int `json:"machine_window_open_seconds"`
		MachineWindowClickSeconds    *int `json:"machine_window_click_seconds"`
		MachineWindowProbableSeconds *int `json:"machine_window_probable_seconds"`
	}{MachineWindowOpenSeconds: &open, MachineWindowClickSeconds: &click, MachineWindowProbableSeconds: &probable}}

	got := patch.Apply(doc)
	got.Normalize()
	if got.Tracking.MachineWindowOpenSeconds != open {
		t.Errorf("MachineWindowOpenSeconds = %d, want %d", got.Tracking.MachineWindowOpenSeconds, open)
	}
	if got.Tracking.MachineWindowClickSeconds != click {
		t.Errorf("MachineWindowClickSeconds = %d, want %d", got.Tracking.MachineWindowClickSeconds, click)
	}
	// Half an hour is past the 900s ceiling the other two windows have, so
	// this only survives if the probable window is clamped on its own bounds.
	if got.Tracking.MachineWindowProbableSeconds != probable {
		t.Errorf("MachineWindowProbableSeconds = %d, want %d", got.Tracking.MachineWindowProbableSeconds, probable)
	}

	// An absent section keeps what is stored rather than clearing it.
	kept := Patch{}.Apply(got)
	if kept.Tracking != got.Tracking {
		t.Errorf("absent tracking section changed the document: %+v", kept.Tracking)
	}
}
