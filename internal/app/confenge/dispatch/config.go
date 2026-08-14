package dispatch

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvGlobalSendsPerHour  = "CONFENGE_GLOBAL_SENDS_PER_HOUR"
	EnvMinSendGapSeconds   = "CONFENGE_MIN_SEND_GAP_SECONDS"
	EnvSendTimezone        = "CONFENGE_SEND_TIMEZONE"
	EnvSendWindowStart     = "CONFENGE_SEND_WINDOW_START"
	EnvSendWindowEnd       = "CONFENGE_SEND_WINDOW_END"
	EnvDispatchPaused      = "CONFENGE_DISPATCH_PAUSED"
	EnvDispatchPauseReason = "CONFENGE_DISPATCH_PAUSE_REASON"
	EnvBusinessDaysOnly    = "CONFENGE_SEND_BUSINESS_DAYS_ONLY"
	EnvRateMode            = "CONFENGE_RATE_MODE"
	EnvRateStartPerHour    = "CONFENGE_RATE_START_PER_HOUR"
	EnvRateMaxPerHour      = "CONFENGE_RATE_MAX_PER_HOUR"
)

func LoadConfig() Config {
	cfg := DefaultConfig()
	if v := strings.TrimSpace(os.Getenv(EnvGlobalSendsPerHour)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.SendsPerHour = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvMinSendGapSeconds)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MinGap = time.Duration(n) * time.Second
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvSendTimezone)); v != "" {
		cfg.Timezone = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvSendWindowStart)); v != "" {
		cfg.WindowStart = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvSendWindowEnd)); v != "" {
		cfg.WindowEnd = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvDispatchPaused)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.EnvPaused = b
		}
	}
	cfg.EnvPauseReason = strings.TrimSpace(os.Getenv(EnvDispatchPauseReason))
	// Business days default true; only disable with explicit false.
	if v := strings.TrimSpace(os.Getenv(EnvBusinessDaysOnly)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.BusinessDaysOnly = b
		}
	} else {
		cfg.BusinessDaysOnly = true
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvRateMode))); v != "" {
		cfg.RateMode = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvRateStartPerHour)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RateStartPerHour = n
			// Adaptive start becomes current cap when not overridden by GLOBAL.
			if strings.TrimSpace(os.Getenv(EnvGlobalSendsPerHour)) == "" {
				cfg.SendsPerHour = n
				cfg.MinGap = MinGapForRate(n)
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvRateMaxPerHour)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RateMaxPerHour = n
		}
	}
	return cfg
}
