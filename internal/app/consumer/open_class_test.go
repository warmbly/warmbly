package jobs

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/repository"
)

func strp(s string) *string { return &s }

// chromeUA is an ordinary desktop browser: the user agent a security gateway
// presents, which is exactly why the UA rules cannot see one.
const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// seen builds the engagement the per-event rules read, with a browser UA and
// no recognised source unless the test says otherwise.
func seen(sent time.Time, at time.Time) engagement {
	return engagement{userAgent: strp(chromeUA), sentAt: &sent, at: at}
}

// from marks the engagement as coming from a source the edge recognised.
func (e engagement) from(label string, probable bool) engagement {
	e.scanner, e.probable = strp(label), probable
	return e
}

// opens and clicks are the window pair each kind of event is judged against.
func opens(t instancesettings.Tracking) (time.Duration, time.Duration) {
	return t.OpenWindow(), t.ProbableWindow(t.OpenWindow())
}

func clicks(t instancesettings.Tracking) (time.Duration, time.Duration) {
	return t.ClickWindow(), t.ProbableWindow(t.ClickWindow())
}

func TestIsInstantUsesTheDispatchClock(t *testing.T) {
	sent := time.Now()
	window := time.Minute
	if !isInstant(&sent, sent.Add(3*time.Second), window) {
		t.Fatal("three seconds after dispatch is a machine")
	}
	if isInstant(&sent, sent.Add(90*time.Second), window) {
		t.Fatal("past the window is a person")
	}
	if isInstant(nil, sent, window) {
		t.Fatal("an unknown dispatch time must never count as instant")
	}
	// The window is a half-open interval, so the boundary itself is already
	// out. Without this the two windows would overlap by a second.
	if isInstant(&sent, sent.Add(window), window) {
		t.Fatal("the boundary is outside the window")
	}
	// A stamp before the dispatch means the two clocks disagree, not that
	// someone read the mail early. The old form compared a raw difference, so
	// every skewed event fell inside the window and was marked automated;
	// the rule abstains instead and lets the user agent and network decide.
	if isInstant(&sent, sent.Add(-time.Hour), window) {
		t.Fatal("an event stamped before dispatch is not instant")
	}
}

// The window is a deployment property, not a constant: the clock starts when
// the send is handed to the worker, so it has to cover provider queueing and
// transit before the recipient's gateway has even seen the message.
func TestMachineWindowsAreOperatorEditable(t *testing.T) {
	sent := time.Now()
	at := sent.Add(90 * time.Second)

	w, p := opens(instancesettings.DefaultTracking())
	if m, _ := classifyOpen(seen(sent, at), w, p); m {
		t.Fatal("ninety seconds is past the shipped open window")
	}
	widened := instancesettings.Tracking{MachineWindowOpenSeconds: 120}
	widened.Normalize()
	w, p = opens(widened)
	if m, r := classifyOpen(seen(sent, at), w, p); !m || r != repository.EmailOpenReasonInstant {
		t.Fatalf("a widened window catches it, got %v %q", m, r)
	}
}

// Opens and clicks are tuned separately because the two mistakes cost
// different things: a misjudged open loses a metric, a misjudged click loses
// the automation behind an interested lead.
func TestOpenAndClickWindowsAreIndependent(t *testing.T) {
	sent := time.Now()
	windows := instancesettings.DefaultTracking()
	at := sent.Add(45 * time.Second)

	ow, op := opens(windows)
	if m, r := classifyOpen(seen(sent, at), ow, op); !m || r != repository.EmailOpenReasonInstant {
		t.Fatalf("forty-five seconds is inside the shipped open window, got %v %q", m, r)
	}
	cw, cp := clicks(windows)
	if m, r := classifyClick(seen(sent, at), cw, cp); m || r != "" {
		t.Fatalf("the same moment is outside the shipped click window, got %v %q", m, r)
	}
}

func TestClassifyClick(t *testing.T) {
	sent := time.Now()
	w, p := clicks(instancesettings.DefaultTracking())

	bare := seen(sent, sent.Add(time.Minute))
	bare.userAgent = nil
	if m, r := classifyClick(bare, w, p); !m || r != repository.LinkClickReasonPrefetch {
		t.Fatalf("no user agent = prefetch, got %v %q", m, r)
	}
	if m, r := classifyClick(seen(sent, sent.Add(2*time.Second)), w, p); !m || r != repository.LinkClickReasonInstant {
		t.Fatalf("a browser UA two seconds after dispatch = instant, got %v %q", m, r)
	}
	if m, r := classifyClick(seen(sent, sent.Add(time.Minute)), w, p); m || r != "" {
		t.Fatalf("a browser a minute later is a person, got %v %q", m, r)
	}
}

// A security gateway walks a message with an ordinary browser's user agent
// and can do it long after delivery, so neither the UA rules nor the machine
// window sees it. The edge's verdict on the source network is what does, and
// it outranks both: a Chrome UA from a mail-filtering network is still a scan.
func TestClassifyScannerSourceOutranksTheUserAgent(t *testing.T) {
	sent := time.Now()
	late := sent.Add(time.Hour)
	windows := instancesettings.DefaultTracking()
	ow, op := opens(windows)
	cw, cp := clicks(windows)

	if m, r := classifyClick(seen(sent, late).from("microsoft-365-protection", false), cw, cp); !m || r != repository.LinkClickReasonScanner {
		t.Fatalf("a click from a scanner network = scanner, got %v %q", m, r)
	}
	if m, r := classifyOpen(seen(sent, late).from("microsoft-365-protection", false), ow, op); !m || r != repository.EmailOpenReasonScanner {
		t.Fatalf("an open from a scanner network = scanner, got %v %q", m, r)
	}
	// An empty label is the same as none: the edge recognised nothing, and a
	// blank string must not silently condemn every event that carries it.
	if m, r := classifyOpen(seen(sent, late).from("  ", false), ow, op); m || r != "" {
		t.Fatalf("a blank scanner label is not a verdict, got %v %q", m, r)
	}
	// Nor may the probable flag alone condemn one: it qualifies a label, and
	// with no label there is nothing to qualify.
	if m, r := classifyOpen(seen(sent, late).from("  ", true), ow, op); m || r != "" {
		t.Fatalf("a blank label is not a verdict when probable either, got %v %q", m, r)
	}
	if m, r := classifyClick(seen(sent, late), cw, cp); m || r != "" {
		t.Fatalf("no scanner label leaves the click a person's, got %v %q", m, r)
	}
}

// Proofpoint and Mimecast run browser isolation: a click ticket walked from
// their networks may be the delivery-time scan or a person reading the page
// their gateway rendered. The label cannot settle that, so it widens the
// window instead. Inside it the event is the scan; outside it the person.
func TestProbableScannerWidensTheWindowInsteadOfDeciding(t *testing.T) {
	sent := time.Now()
	windows := instancesettings.DefaultTracking()
	cw, cp := clicks(windows)
	ow, op := opens(windows)

	// A minute after dispatch is well past the 30s click window, so without
	// the label this is a person. With it, it is the arrival scan.
	scan := sent.Add(time.Minute)
	if m, r := classifyClick(seen(sent, scan), cw, cp); m {
		t.Fatalf("a minute later with no label is a person, got %v %q", m, r)
	}
	if m, r := classifyClick(seen(sent, scan).from("proofpoint", true), cw, cp); !m || r != repository.LinkClickReasonScanner {
		t.Fatalf("inside the probable window it is the scan, got %v %q", m, r)
	}
	if m, r := classifyOpen(seen(sent, scan).from("proofpoint", true), ow, op); !m || r != repository.EmailOpenReasonScanner {
		t.Fatalf("the same for an open, got %v %q", m, r)
	}

	// An hour later the person has read their mail and clicked through
	// isolation. A certain label would take that click and the automation
	// behind it; a probable one must not.
	late := sent.Add(time.Hour)
	if m, r := classifyClick(seen(sent, late).from("proofpoint", true), cw, cp); m || r != "" {
		t.Fatalf("past the probable window an isolated click is a person's, got %v %q", m, r)
	}
	if m, r := classifyOpen(seen(sent, late).from("proofpoint", true), ow, op); m || r != "" {
		t.Fatalf("past the probable window an isolated open is a person's, got %v %q", m, r)
	}
	// The same source marked certain is the behaviour we deliberately did not
	// ship, and the contrast is the whole point of the flag.
	if m, _ := classifyClick(seen(sent, late).from("proofpoint", false), cw, cp); !m {
		t.Fatal("a certain label still decides on its own")
	}
}

// Naming a network may only ever catch more scans. A probable window set
// shorter than the window the event would get anyway must not hand a scan
// back: the classifier takes the wider of the two.
func TestAProbableLabelNeverWeakensTheOrdinaryWindow(t *testing.T) {
	sent := time.Now()
	narrow := instancesettings.Tracking{
		MachineWindowOpenSeconds:     60,
		MachineWindowClickSeconds:    30,
		MachineWindowProbableSeconds: 1,
	}
	narrow.Normalize()
	cw, cp := clicks(narrow)
	if cp < cw {
		t.Fatalf("the probable window may not fall below the click window: %v < %v", cp, cw)
	}
	at := sent.Add(5 * time.Second)
	if m, _ := classifyClick(seen(sent, at).from("proofpoint", true), cw, cp); !m {
		t.Fatal("a probable source inside the ordinary window is still a machine")
	}
	if m, _ := classifyClick(seen(sent, at), cw, cp); !m {
		t.Fatal("and so is an unlabelled one")
	}
}

// The window is its own setting with its own bounds, because how long a
// security vendor takes to detonate a link is the vendor's property, not the
// instance's. Zero means the compiled default, as everywhere else here.
func TestProbableWindowIsOperatorEditable(t *testing.T) {
	var zero instancesettings.Tracking
	zero.Normalize()
	if got := zero.MachineWindowProbableSeconds; got != config.TrackingMachineWindowProbableSecondsDefault {
		t.Fatalf("an unwritten section takes the compiled default, got %d", got)
	}
	over := instancesettings.Tracking{MachineWindowProbableSeconds: config.TrackingMachineWindowProbableSecondsMax + 1}
	over.Normalize()
	if got := over.MachineWindowProbableSeconds; got != config.TrackingMachineWindowProbableSecondsMax {
		t.Fatalf("above the ceiling clamps to it, got %d", got)
	}
	// It reaches past the 900s ceiling the other two windows have, which is
	// the point: a vendor may detonate a link hours after delivery.
	if config.TrackingMachineWindowProbableSecondsMax <= config.TrackingMachineWindowSecondsMax {
		t.Fatal("the probable window must be allowed to exceed the ordinary ones")
	}
	wide := instancesettings.Tracking{MachineWindowProbableSeconds: 7200}
	wide.Normalize()
	sent := time.Now()
	cw, cp := clicks(wide)
	if m, _ := classifyClick(seen(sent, sent.Add(time.Hour)).from("mimecast", true), cw, cp); !m {
		t.Fatal("a two-hour probable window catches an hour-old scan")
	}
}

func TestEventTimeFallsBackToNow(t *testing.T) {
	stamp := "2026-09-03T10:00:00Z"
	if got := eventTime(stamp); !got.Equal(time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected parse: %v", got)
	}
	if d := time.Since(eventTime("garbage")); d < 0 || d > time.Minute {
		t.Fatalf("unreadable stamp should fall back to now, got %v ago", d)
	}
}
