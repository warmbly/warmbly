package models

import (
	"time"

	"github.com/google/uuid"
)

// Human sending behaviour. A mailbox that sends exactly 40 emails a day, every
// day, starting at 09:00:00 and spaced exactly 600s apart is describing itself
// as a machine in its own Received headers. These settings give each mailbox a
// range to roll inside, and the roll happens once per local day so the mailbox
// behaves like one person having one workday rather than a process re-deciding
// its schedule every few minutes.
//
// Every minute-of-day value below is minutes since LOCAL midnight in the
// mailbox's own timezone (email_accounts.timezone).
const (
	BehaviorDailyLimitMinDefault = 30
	BehaviorDailyLimitMaxDefault = 45
	BehaviorDailyLimitFloor      = 1
	BehaviorDailyLimitCeiling    = 500

	// Hourly ceiling for cold sends. The default band is deliberately wider
	// than daily_limit / working-hours so the hourly cap shapes bursts without
	// making the daily target unreachable.
	BehaviorHourlyLimitMinDefault = 5
	BehaviorHourlyLimitMaxDefault = 9
	BehaviorHourlyLimitFloor      = 1
	BehaviorHourlyLimitCeiling    = 200

	BehaviorGapMinSecondsDefault = 90
	BehaviorGapMaxSecondsDefault = 420
	BehaviorGapFloor             = 30
	BehaviorGapCeiling           = 86400

	// 09:03-09:27 and 17:18-17:56.
	BehaviorWorkStartMinDefault = 543
	BehaviorWorkStartMaxDefault = 567
	BehaviorWorkEndMinDefault   = 1038
	BehaviorWorkEndMaxDefault   = 1076

	// Lunch starts somewhere in 12:00-13:30 and runs 30-60 minutes.
	BehaviorLunchEarliestDefault   = 720
	BehaviorLunchLatestDefault     = 810
	BehaviorLunchMinMinutesDefault = 30
	BehaviorLunchMaxMinutesDefault = 60
	BehaviorLunchMaxMinutesCeiling = 240

	// Monday-indexed bitmask, bit 0 = Monday. 31 = Mon-Fri.
	BehaviorWeekdaysDefault = 31
	BehaviorWeekdaysAll     = 127

	// MinutesPerDay is the exclusive upper bound for a minute-of-day value.
	MinutesPerDay = 1440
)

// SendingBehavior is one mailbox's behaviour profile: the ranges, not the
// rolled values. Disabled profiles are still stored so a customer can tune the
// ranges before switching it on.
type SendingBehavior struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Enabled        bool      `json:"enabled"`

	DailyLimitMin int `json:"daily_limit_min"`
	DailyLimitMax int `json:"daily_limit_max"`

	HourlyLimitMin int `json:"hourly_limit_min"`
	HourlyLimitMax int `json:"hourly_limit_max"`

	GapMinSeconds int `json:"gap_min_seconds"`
	GapMaxSeconds int `json:"gap_max_seconds"`

	WorkStartMin int `json:"work_start_min"`
	WorkStartMax int `json:"work_start_max"`
	WorkEndMin   int `json:"work_end_min"`
	WorkEndMax   int `json:"work_end_max"`

	LunchEnabled    bool `json:"lunch_enabled"`
	LunchEarliest   int  `json:"lunch_earliest"`
	LunchLatest     int  `json:"lunch_latest"`
	LunchMinMinutes int  `json:"lunch_min_minutes"`
	LunchMaxMinutes int  `json:"lunch_max_minutes"`

	// Monday-indexed bitmask (bit 0 = Monday .. bit 6 = Sunday), matching
	// campaigns.days and the dashboard's Monday-first week grid.
	Weekdays int `json:"weekdays"`

	// Timezone is a read-only echo of the mailbox's timezone, so a client
	// rendering the profile does not have to fetch the mailbox as well.
	Timezone string `json:"timezone,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultSendingBehavior is the profile a mailbox gets before anyone edits it:
// switched off, with ranges that describe an ordinary weekday sender.
func DefaultSendingBehavior(accountID uuid.UUID) SendingBehavior {
	return SendingBehavior{
		EmailAccountID:  accountID,
		Enabled:         false,
		DailyLimitMin:   BehaviorDailyLimitMinDefault,
		DailyLimitMax:   BehaviorDailyLimitMaxDefault,
		HourlyLimitMin:  BehaviorHourlyLimitMinDefault,
		HourlyLimitMax:  BehaviorHourlyLimitMaxDefault,
		GapMinSeconds:   BehaviorGapMinSecondsDefault,
		GapMaxSeconds:   BehaviorGapMaxSecondsDefault,
		WorkStartMin:    BehaviorWorkStartMinDefault,
		WorkStartMax:    BehaviorWorkStartMaxDefault,
		WorkEndMin:      BehaviorWorkEndMinDefault,
		WorkEndMax:      BehaviorWorkEndMaxDefault,
		LunchEnabled:    true,
		LunchEarliest:   BehaviorLunchEarliestDefault,
		LunchLatest:     BehaviorLunchLatestDefault,
		LunchMinMinutes: BehaviorLunchMinMinutesDefault,
		LunchMaxMinutes: BehaviorLunchMaxMinutesDefault,
		Weekdays:        BehaviorWeekdaysDefault,
	}
}

// WorksOn reports whether the profile sends on the given weekday. The stored
// mask is Monday-indexed while time.Weekday is Sunday-indexed, so the mapping
// is explicit here and nowhere else.
func (b SendingBehavior) WorksOn(wd time.Weekday) bool {
	return b.Weekdays&(1<<weekdayBit(wd)) != 0
}

// weekdayBit maps time.Weekday (Sun=0) onto the Monday-indexed bit position
// used by every weekday mask in this codebase.
func weekdayBit(wd time.Weekday) uint {
	return uint((int(wd) + 6) % 7)
}

// UpdateSendingBehavior is the PUT body. Every field is optional; omitted
// fields keep their stored value, so a client can toggle `enabled` without
// resending the whole profile.
type UpdateSendingBehavior struct {
	Enabled *bool `json:"enabled"`

	DailyLimitMin *int `json:"daily_limit_min"`
	DailyLimitMax *int `json:"daily_limit_max"`

	HourlyLimitMin *int `json:"hourly_limit_min"`
	HourlyLimitMax *int `json:"hourly_limit_max"`

	GapMinSeconds *int `json:"gap_min_seconds"`
	GapMaxSeconds *int `json:"gap_max_seconds"`

	WorkStartMin *int `json:"work_start_min"`
	WorkStartMax *int `json:"work_start_max"`
	WorkEndMin   *int `json:"work_end_min"`
	WorkEndMax   *int `json:"work_end_max"`

	LunchEnabled    *bool `json:"lunch_enabled"`
	LunchEarliest   *int  `json:"lunch_earliest"`
	LunchLatest     *int  `json:"lunch_latest"`
	LunchMinMinutes *int  `json:"lunch_min_minutes"`
	LunchMaxMinutes *int  `json:"lunch_max_minutes"`

	Weekdays *int `json:"weekdays"`
}

// Apply overlays the patch onto a profile. It does not validate; call
// Validate on the result.
func (u UpdateSendingBehavior) Apply(b SendingBehavior) SendingBehavior {
	setInt := func(dst *int, src *int) {
		if src != nil {
			*dst = *src
		}
	}
	if u.Enabled != nil {
		b.Enabled = *u.Enabled
	}
	if u.LunchEnabled != nil {
		b.LunchEnabled = *u.LunchEnabled
	}
	setInt(&b.DailyLimitMin, u.DailyLimitMin)
	setInt(&b.DailyLimitMax, u.DailyLimitMax)
	setInt(&b.HourlyLimitMin, u.HourlyLimitMin)
	setInt(&b.HourlyLimitMax, u.HourlyLimitMax)
	setInt(&b.GapMinSeconds, u.GapMinSeconds)
	setInt(&b.GapMaxSeconds, u.GapMaxSeconds)
	setInt(&b.WorkStartMin, u.WorkStartMin)
	setInt(&b.WorkStartMax, u.WorkStartMax)
	setInt(&b.WorkEndMin, u.WorkEndMin)
	setInt(&b.WorkEndMax, u.WorkEndMax)
	setInt(&b.LunchEarliest, u.LunchEarliest)
	setInt(&b.LunchLatest, u.LunchLatest)
	setInt(&b.LunchMinMinutes, u.LunchMinMinutes)
	setInt(&b.LunchMaxMinutes, u.LunchMaxMinutes)
	setInt(&b.Weekdays, u.Weekdays)
	return b
}

// BehaviorValidationError names the field that failed and why, so the API can
// return something more useful than "invalid body".
type BehaviorValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *BehaviorValidationError) Error() string { return e.Field + ": " + e.Message }

// Validate mirrors the CHECK constraints on email_account_behavior, plus the
// cross-field rules the database expresses as one opaque constraint. The point
// is that a bad profile is rejected with a field name at the API boundary, not
// as a 500 from a constraint violation.
func (b SendingBehavior) Validate() error {
	fail := func(field, msg string) error {
		return &BehaviorValidationError{Field: field, Message: msg}
	}

	if b.DailyLimitMin < BehaviorDailyLimitFloor || b.DailyLimitMax > BehaviorDailyLimitCeiling {
		return fail("daily_limit", "must be between 1 and 500 emails per day")
	}
	if b.DailyLimitMin > b.DailyLimitMax {
		return fail("daily_limit", "minimum cannot be above maximum")
	}

	if b.HourlyLimitMin < BehaviorHourlyLimitFloor || b.HourlyLimitMax > BehaviorHourlyLimitCeiling {
		return fail("hourly_limit", "must be between 1 and 200 emails per hour")
	}
	if b.HourlyLimitMin > b.HourlyLimitMax {
		return fail("hourly_limit", "minimum cannot be above maximum")
	}

	if b.GapMinSeconds < BehaviorGapFloor || b.GapMaxSeconds > BehaviorGapCeiling {
		return fail("gap_seconds", "must be between 30 seconds and 24 hours")
	}
	if b.GapMinSeconds > b.GapMaxSeconds {
		return fail("gap_seconds", "minimum cannot be above maximum")
	}

	if b.WorkStartMin < 0 || b.WorkStartMax >= MinutesPerDay || b.WorkStartMin > b.WorkStartMax {
		return fail("work_start", "must be a valid time range within the day")
	}
	if b.WorkEndMin < 0 || b.WorkEndMax >= MinutesPerDay || b.WorkEndMin > b.WorkEndMax {
		return fail("work_end", "must be a valid time range within the day")
	}
	// The latest start must still precede the earliest end, or some rolls
	// would produce a workday that ends before it begins.
	if b.WorkStartMax >= b.WorkEndMin {
		return fail("work_end", "the workday must end after the latest possible start")
	}

	if b.LunchEarliest < 0 || b.LunchLatest >= MinutesPerDay || b.LunchEarliest > b.LunchLatest {
		return fail("lunch_window", "must be a valid time range within the day")
	}
	if b.LunchMinMinutes < 0 || b.LunchMaxMinutes > BehaviorLunchMaxMinutesCeiling || b.LunchMinMinutes > b.LunchMaxMinutes {
		return fail("lunch_length", "must be between 0 and 240 minutes")
	}
	if b.LunchEnabled {
		// A break outside the workday is silently ignored by the planner,
		// which reads as "lunch is on but nothing happens". Reject it instead.
		if b.LunchEarliest < b.WorkStartMax || b.LunchLatest+b.LunchMaxMinutes > b.WorkEndMin {
			return fail("lunch_window", "the break must fit inside the shortest possible workday")
		}
	}

	if b.Weekdays < 0 || b.Weekdays > BehaviorWeekdaysAll {
		return fail("weekdays", "must be a bitmask of Monday..Sunday")
	}
	if b.Enabled && b.Weekdays == 0 {
		return fail("weekdays", "pick at least one sending day")
	}

	return nil
}

// DailyPlan is the workday a mailbox actually rolled for one local date. It is
// written once and never updated, so every scheduling pass through the day
// reads the same numbers.
type DailyPlan struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	// PlanDate is the local calendar date in Timezone, as YYYY-MM-DD.
	PlanDate string `json:"plan_date"`
	Timezone string `json:"timezone"`

	IsWorkingDay bool `json:"is_working_day"`

	DailyLimit       int  `json:"daily_limit"`
	HourlyLimit      int  `json:"hourly_limit"`
	WorkStartMinute  int  `json:"work_start_minute"`
	WorkEndMinute    int  `json:"work_end_minute"`
	LunchStartMinute *int `json:"lunch_start_minute"`
	LunchEndMinute   *int `json:"lunch_end_minute"`
	GapMinSeconds    int  `json:"gap_min_seconds"`
	GapMaxSeconds    int  `json:"gap_max_seconds"`

	CreatedAt time.Time `json:"created_at"`
}

// HasLunch reports whether this day carries a break.
func (p DailyPlan) HasLunch() bool {
	return p.LunchStartMinute != nil && p.LunchEndMinute != nil
}

// WorkingMinutes is the length of the day's sending window with the break
// removed. Used to pace sends across the day.
func (p DailyPlan) WorkingMinutes() int {
	if !p.IsWorkingDay {
		return 0
	}
	total := p.WorkEndMinute - p.WorkStartMinute
	if p.HasLunch() {
		total -= *p.LunchEndMinute - *p.LunchStartMinute
	}
	if total < 0 {
		return 0
	}
	return total
}

// Contains reports whether a minute-of-day falls inside the working window and
// outside the break.
func (p DailyPlan) Contains(minute int) bool {
	if !p.IsWorkingDay || minute < p.WorkStartMinute || minute >= p.WorkEndMinute {
		return false
	}
	if p.HasLunch() && minute >= *p.LunchStartMinute && minute < *p.LunchEndMinute {
		return false
	}
	return true
}

// NextOpenMinute returns the first sending minute at or after `minute` within
// this day, and false when the day is over (or is not a working day). A minute
// inside the break jumps to the end of the break.
func (p DailyPlan) NextOpenMinute(minute int) (int, bool) {
	if !p.IsWorkingDay {
		return 0, false
	}
	if minute < p.WorkStartMinute {
		minute = p.WorkStartMinute
	}
	if p.HasLunch() && minute >= *p.LunchStartMinute && minute < *p.LunchEndMinute {
		minute = *p.LunchEndMinute
	}
	if minute >= p.WorkEndMinute {
		return 0, false
	}
	return minute, true
}

// DailyPlanView is the plan plus the derived numbers the dashboard shows, so
// the client never re-implements the window maths.
type DailyPlanView struct {
	DailyPlan
	// SentToday is completed cold sends from this mailbox on this local date.
	SentToday int `json:"sent_today"`
	// RemainingToday is what the plan still allows, floored at zero.
	RemainingToday int `json:"remaining_today"`
	// Behavior is the profile the plan was rolled from.
	Behavior SendingBehavior `json:"behavior"`
}
