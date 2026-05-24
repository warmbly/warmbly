package scheduler

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

// SchedulerService provides task scheduling functionality
type SchedulerService interface {
	// Warmup scheduling
	CalculateNextWarmupTime(ctx context.Context, accountID uuid.UUID) (time.Time, error)

	// Campaign scheduling
	CalculateNextCampaignTime(ctx context.Context, campaignID uuid.UUID) (time.Time, *repository.ContactSequencePair, uuid.UUID, error)

	// Email scheduling (smart send)
	CalculateNextEmailTime(ctx context.Context, accountID uuid.UUID) (time.Time, error)
}

type schedulerService struct {
	taskRepo             repository.TaskRepository
	warmupRepo           repository.WarmupRepository
	campaignProgressRepo repository.CampaignProgressRepository
	emailRepo            repository.EmailRepository
	campaignRepo         repository.CampaignRepository
}

// NewSchedulerService creates a new scheduler service
func NewSchedulerService(
	taskRepo repository.TaskRepository,
	warmupRepo repository.WarmupRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	emailRepo repository.EmailRepository,
	campaignRepo repository.CampaignRepository,
) SchedulerService {
	return &schedulerService{
		taskRepo:             taskRepo,
		warmupRepo:           warmupRepo,
		campaignProgressRepo: campaignProgressRepo,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
	}
}

// Timezone utilities

// loadLocation safely loads a timezone location with fallback to UTC
func loadLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// convertToAccountTimezone converts a time from campaign timezone to account timezone
func convertToAccountTimezone(t time.Time, campaignTZ, accountTZ string) time.Time {
	campaignLoc := loadLocation(campaignTZ)
	accountLoc := loadLocation(accountTZ)

	// Get the wall clock time in campaign timezone
	year, month, day := t.In(campaignLoc).Date()
	hour, min, sec := t.In(campaignLoc).Clock()

	// Reconstruct in account timezone
	return time.Date(year, month, day, hour, min, sec, 0, accountLoc)
}

// Time calculation helpers

// parseTimeOfDay parses a time string like "08:00" and returns minutes since midnight
func parseTimeOfDay(timeStr string) int {
	if timeStr == "" {
		return 0
	}

	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return 0
	}

	return t.Hour()*60 + t.Minute()
}

// calculateBusinessHoursRemaining calculates hours remaining in business day
func calculateBusinessHoursRemaining(timezone string) float64 {
	loc := loadLocation(timezone)
	now := time.Now().In(loc)

	// Business hours: 8am to 8pm (12 hours)
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, loc)

	if now.After(endOfDay) {
		return 0
	}

	remaining := endOfDay.Sub(now).Hours()
	return max(0, remaining)
}

// calculateFirstSlotTomorrow calculates first business hour slot tomorrow
func calculateFirstSlotTomorrow(timezone string) time.Time {
	loc := loadLocation(timezone)
	now := time.Now().In(loc)

	// Tomorrow at 8am + random jitter (0-60 minutes)
	tomorrow := now.Add(24 * time.Hour)
	firstSlot := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 8, 0, 0, 0, loc)

	// Add random jitter
	jitter := randomJitter(0, 60)
	return firstSlot.Add(time.Minute * time.Duration(jitter))
}

// randomJitter generates random jitter between min and max minutes
func randomJitter(min, max int) int {
	if min >= max {
		return min
	}
	return min + rand.Intn(max-min)
}

// roundToNearestMinute rounds time to nearest N minutes
func roundToNearestMinute(t time.Time, minutes int) time.Time {
	if minutes <= 0 {
		return t
	}

	rounded := t.Truncate(time.Minute * time.Duration(minutes))
	if t.Sub(rounded) >= time.Minute*time.Duration(minutes)/2 {
		rounded = rounded.Add(time.Minute * time.Duration(minutes))
	}

	return rounded
}
