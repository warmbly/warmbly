package models

import (
	"strconv"
	"strings"
)

// ClockMinutes parses "HH:MM" into minutes since midnight, tolerating trailing
// seconds and a fraction. Returns fallback for anything it cannot read.
//
// The tolerance is the point: start_time, end_time, warmup_start_time and
// warmup_end_time are Postgres `time` columns that arrive as "09:00:00.000000",
// while the app writes "09:00". A parser that accepted only the second form
// silently disabled every campaign's sending window. One definition, so a
// second caller cannot reintroduce that.
func ClockMinutes(v string, fallback int) int {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) < 2 {
		return fallback
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return fallback
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return fallback
	}
	return hour*60 + minute
}
