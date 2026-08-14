package confenge

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
)

// TestLoadConfigHonorsOperatorDailyLimit proves env is the single runtime
// source of truth: operator CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT is never
// silently replaced by Go package defaults.
func TestLoadConfigHonorsOperatorDailyLimit(t *testing.T) {
	t.Setenv(EnvDefaultDailyLimit, "42")
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "10")
	t.Setenv(dispatch.EnvMinSendGapSeconds, "360")

	cfg := LoadConfig()
	if cfg.DefaultDailyLimit != 42 {
		t.Fatalf("DefaultDailyLimit want 42 (operator env), got %d", cfg.DefaultDailyLimit)
	}
	dcfg := dispatch.LoadConfig()
	if dcfg.SendsPerHour != 10 {
		t.Fatalf("SendsPerHour want 10, got %d", dcfg.SendsPerHour)
	}
	if int(dcfg.MinGap.Seconds()) != 360 {
		t.Fatalf("MinGap want 360s, got %v", dcfg.MinGap)
	}
}

// TestLoadConfigDefaultsMatchCanonicalSemantics documents the ship defaults:
// hourly governor 10 (adaptive start), min gap 360s, campaign daily secondary ceiling 200
// (headroom for adaptive peak 20/h × 9h = 180).
func TestLoadConfigDefaultsMatchCanonicalSemantics(t *testing.T) {
	t.Setenv(EnvDefaultDailyLimit, "")
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "")
	t.Setenv(dispatch.EnvMinSendGapSeconds, "")
	t.Setenv(dispatch.EnvSendWindowStart, "")
	t.Setenv(dispatch.EnvSendWindowEnd, "")

	cfg := LoadConfig()
	dcfg := dispatch.LoadConfig()
	if DefaultCampaignDailyLimit != 200 {
		t.Fatalf("package DefaultCampaignDailyLimit want 200, got %d", DefaultCampaignDailyLimit)
	}
	if cfg.DefaultDailyLimit != 200 {
		t.Fatalf("LoadConfig default daily want 200, got %d", cfg.DefaultDailyLimit)
	}
	if dispatch.DefaultSendsPerHour != 10 {
		t.Fatalf("package DefaultSendsPerHour want 10, got %d", dispatch.DefaultSendsPerHour)
	}
	if dcfg.SendsPerHour != 10 {
		t.Fatalf("LoadConfig default hourly want 10, got %d", dcfg.SendsPerHour)
	}
	if int(dcfg.MinGap.Seconds()) != 360 {
		t.Fatalf("default min gap want 360s, got %v", dcfg.MinGap)
	}
}

// TestScenarioAHourlyPrimaryDailySecondary: hourly=10 daily=100 window=09-18
// → primary cap is 10/h; 11th of the day is NOT blocked merely for being #11.
func TestScenarioAHourlyPrimaryDailySecondary(t *testing.T) {
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "10")
	t.Setenv(dispatch.EnvMinSendGapSeconds, "360")
	t.Setenv(dispatch.EnvSendWindowStart, "09:00")
	t.Setenv(dispatch.EnvSendWindowEnd, "18:00")
	t.Setenv(EnvDefaultDailyLimit, "100")

	cfg := LoadConfig()
	dcfg := dispatch.LoadConfig()
	if dcfg.SendsPerHour != 10 {
		t.Fatalf("hourly primary want 10, got %d", dcfg.SendsPerHour)
	}
	if cfg.DefaultDailyLimit != 100 {
		t.Fatalf("daily secondary want 100, got %d", cfg.DefaultDailyLimit)
	}
	// Operational primary: 11th within rolling hour blocked by governor (unit-tested
	// in dispatch.TestCap10Blocks11th). Daily 100 must not treat send #11 of the day
	// as over daily budget.
	if cfg.DefaultDailyLimit <= 10 {
		t.Fatalf("daily=%d would collapse ~10/h operation to 10/day", cfg.DefaultDailyLimit)
	}
	// Send #11 of the day is under daily ceiling.
	const daySendN = 11
	if daySendN > cfg.DefaultDailyLimit {
		t.Fatalf("send #%d of day must not be blocked by daily=%d alone", daySendN, cfg.DefaultDailyLimit)
	}
	rep := RunPreflight(cfg, PreflightDeps{})
	for _, c := range rep.Checks {
		if c.Name == "campaign_daily_limit" && c.Severity != CheckPass {
			t.Fatalf("scenario A preflight campaign_daily_limit want pass, got %s: %s", c.Severity, c.Message)
		}
		if c.Name == "governor_hourly" && c.Severity != CheckPass {
			t.Fatalf("scenario A governor_hourly want pass, got %s: %s", c.Severity, c.Message)
		}
	}
}

// TestScenarioBDailyEqualsHourlyWarns: hourly=10 daily=10 → explicit WARN,
// never silent PASS that pretends ~10/h is available.
func TestScenarioBDailyEqualsHourlyWarns(t *testing.T) {
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "10")
	t.Setenv(dispatch.EnvSendWindowStart, "09:00")
	t.Setenv(dispatch.EnvSendWindowEnd, "18:00")

	cfg := Config{
		Enabled:              true,
		RequireHumanApproval: true,
		AutoSendEnabled:      false,
		DefaultDailyLimit:    10,
		MaxInitialEmailWords: 120,
	}
	rep := RunPreflight(cfg, PreflightDeps{})
	var found bool
	for _, c := range rep.Checks {
		if c.Name != "campaign_daily_limit" {
			continue
		}
		found = true
		if c.Severity != CheckWarn && c.Severity != CheckFail {
			t.Fatalf("scenario B want WARN or FAIL, got %s: %s", c.Severity, c.Message)
		}
		msg := strings.ToLower(c.Message)
		if !strings.Contains(msg, "10") {
			t.Fatalf("scenario B message must state effective 10/day: %s", c.Message)
		}
		if !strings.Contains(msg, "collaps") && !strings.Contains(msg, "effective") {
			t.Fatalf("scenario B message must warn about collapse/effective capacity: %s", c.Message)
		}
	}
	if !found {
		t.Fatal("scenario B missing campaign_daily_limit check")
	}
}

// TestMakefileDoesNotHardcodeDailyLimit10 guards the known confenge-local
// footgun where CONFENGE_DEV_ENV forced DAILY_LIMIT=10 over .env.confenge.
func TestMakefileDoesNotHardcodeDailyLimit10(t *testing.T) {
	root := findWarmblyRepoRoot(t)
	mk := filepath.Join(root, "Makefile")
	raw, err := os.ReadFile(mk)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// Forbidden: hard assignment CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=10
	// (without shell default expansion that preserves operator env).
	bad := regexp.MustCompile(`(?m)CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=10\s*\\?\s*$`)
	if bad.MatchString(text) {
		t.Fatal("Makefile hardcodes CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=10; use $${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100}")
	}
	// Required: operator-preserving default of 100 or 200
	has100 := strings.Contains(text, "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=$${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100}") ||
		strings.Contains(text, "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100}")
	has200 := strings.Contains(text, "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=$${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-200}") ||
		strings.Contains(text, "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-200}")
	if !has100 && !has200 {
		t.Fatal("Makefile must use $${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100|200} so operator env wins")
	}
	// .env.confenge.example documents daily ceiling (100 legacy or 200 adaptive-ready)
	ex := filepath.Join(root, ".env.confenge.example")
	exRaw, err := os.ReadFile(ex)
	if err != nil {
		t.Fatal(err)
	}
	exs := string(exRaw)
	if !strings.Contains(exs, "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=100") &&
		!strings.Contains(exs, "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=200") {
		t.Fatal(".env.confenge.example must set CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT")
	}
}

// TestMakefileEnvPrecedenceShellSim documents that after sourcing operator
// env, shell $${VAR:-default} keeps operator daily limit (not Makefile 10).
func TestMakefileEnvPrecedenceShellSim(t *testing.T) {
	// Simulate the confenge-local pattern:
	//   set -a; . envfile; set +a;
	//   CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100} printenv
	script := `
set -e
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
printf 'CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=77\nCONFENGE_GLOBAL_SENDS_PER_HOUR=10\n' > "$tmp"
set -a
# shellcheck disable=SC1090
. "$tmp"
set +a
# Makefile-style defaults (must not clobber 77)
export CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT="${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100}"
export CONFENGE_GLOBAL_SENDS_PER_HOUR="${CONFENGE_GLOBAL_SENDS_PER_HOUR:-10}"
echo "daily=$CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT"
echo "hourly=$CONFENGE_GLOBAL_SENDS_PER_HOUR"
# Unset path: default 100
unset CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT
export CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT="${CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT:-100}"
echo "default_daily=$CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT"
`
	out, err := exec.Command("bash", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("shell sim: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "daily=77") {
		t.Fatalf("operator env must win, got:\n%s", s)
	}
	if !strings.Contains(s, "hourly=10") {
		t.Fatalf("hourly missing:\n%s", s)
	}
	if !strings.Contains(s, "default_daily=100") {
		t.Fatalf("unset default must be 100, got:\n%s", s)
	}
}

func findWarmblyRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			if _, err2 := os.Stat(filepath.Join(dir, "go.mod")); err2 == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found from " + wd)
		}
		dir = parent
	}
}
