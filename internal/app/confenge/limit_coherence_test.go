package confenge

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
)

// TestDefaultDailyLimitDoesNotCollapseHourlyGovernor guards the known
// 10/day vs 10/hour incoherence: campaign daily default must leave headroom
// for a full send window at the global hourly governor.
func TestDefaultDailyLimitDoesNotCollapseHourlyGovernor(t *testing.T) {
	dcfg := dispatch.DefaultConfig()
	windowH := SendWindowHours(dcfg.WindowStart, dcfg.WindowEnd)
	if windowH <= 0 {
		t.Fatalf("default send window hours=%d", windowH)
	}
	hourlyDay := dcfg.SendsPerHour * windowH
	if DefaultCampaignDailyLimit < hourlyDay {
		t.Fatalf(
			"DefaultCampaignDailyLimit=%d < hourly×window (%d×%d=%d): daily default would collapse ~%d/h pace to %d/day",
			DefaultCampaignDailyLimit, dcfg.SendsPerHour, windowH, hourlyDay, dcfg.SendsPerHour, DefaultCampaignDailyLimit,
		)
	}
	if DefaultCampaignDailyLimit > 200 {
		t.Fatalf("DefaultCampaignDailyLimit=%d exceeds ValidateStartup max 200", DefaultCampaignDailyLimit)
	}
}

func TestPreflightWarnsWhenDailyCollapsesHourly(t *testing.T) {
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "10")
	t.Setenv(dispatch.EnvMinSendGapSeconds, "360")
	t.Setenv(dispatch.EnvSendWindowStart, "09:00")
	t.Setenv(dispatch.EnvSendWindowEnd, "18:00")

	cfg := Config{
		Enabled:              true,
		RequireHumanApproval: true,
		AutoSendEnabled:      false,
		DefaultDailyLimit:    10, // the bad old default
		MaxInitialEmailWords: 120,
	}
	rep := RunPreflight(cfg, PreflightDeps{})
	var dailyWarn, hourlyPass bool
	for _, c := range rep.Checks {
		if c.Name == "governor_hourly" && c.Severity == CheckPass {
			hourlyPass = true
			if !strings.Contains(c.Message, "10") {
				t.Fatalf("hourly message missing 10: %s", c.Message)
			}
		}
		if c.Name == "campaign_daily_limit" && c.Severity == CheckWarn {
			dailyWarn = true
			if !strings.Contains(c.Message, "collapses") && !strings.Contains(c.Message, "effective") {
				t.Fatalf("expected collapse/effective warning, got: %s", c.Message)
			}
		}
	}
	if !hourlyPass {
		t.Fatal("expected governor_hourly pass")
	}
	if !dailyWarn {
		t.Fatal("expected campaign_daily_limit warn when daily=10 and hourly=10")
	}
}

func TestPreflightPassWhenDailyAllowsHourlyDay(t *testing.T) {
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "10")
	t.Setenv(dispatch.EnvSendWindowStart, "09:00")
	t.Setenv(dispatch.EnvSendWindowEnd, "18:00")

	cfg := Config{
		Enabled:              true,
		RequireHumanApproval: true,
		AutoSendEnabled:      false,
		DefaultDailyLimit:    DefaultCampaignDailyLimit,
		MaxInitialEmailWords: 120,
	}
	rep := RunPreflight(cfg, PreflightDeps{})
	for _, c := range rep.Checks {
		if c.Name == "campaign_daily_limit" && c.Severity != CheckPass {
			t.Fatalf("expected campaign_daily_limit pass with default %d, got %s: %s",
				DefaultCampaignDailyLimit, c.Severity, c.Message)
		}
	}
}

func TestReadinessGovernorCapIsHourly(t *testing.T) {
	t.Setenv(dispatch.EnvGlobalSendsPerHour, "10")
	t.Setenv(dispatch.EnvSendWindowStart, "09:00")
	t.Setenv(dispatch.EnvSendWindowEnd, "18:00")

	cfg := Config{
		Enabled:              true,
		RequireHumanApproval: true,
		DefaultDailyLimit:    100,
	}
	r := BuildReadiness(cfg, ReadinessInputs{})
	if r.GovernorCap != 10 {
		t.Fatalf("GovernorCap want 10/h, got %d", r.GovernorCap)
	}
	if r.CampaignDailyLimit != 100 {
		t.Fatalf("CampaignDailyLimit want 100, got %d", r.CampaignDailyLimit)
	}
	// 10/h * 9h window = 90; daily 100 → effective 90
	if r.EffectiveDailyCap != 90 {
		t.Fatalf("EffectiveDailyCap want 90, got %d", r.EffectiveDailyCap)
	}
}

func TestSendWindowHours(t *testing.T) {
	if got := SendWindowHours("09:00", "18:00"); got != 9 {
		t.Fatalf("09-18 want 9, got %d", got)
	}
	if got := SendWindowHours("09:00", "17:00"); got != 8 {
		t.Fatalf("09-17 want 8, got %d", got)
	}
	if got := SendWindowHours("bad", "18:00"); got != 0 {
		t.Fatalf("bad start want 0, got %d", got)
	}
}
