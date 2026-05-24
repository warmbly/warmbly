package validate

import (
	"time"

	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/errx"
)

func CampaignName(name string) *errx.Error {
	l := len(name)
	if l < 3 || l > 50 {
		return errx.ErrCampaignName
	}
	return nil
}

func CampaignDescription(description string) *errx.Error {
	if len(description) > 300 {
		return errx.ErrCampaignDescription
	}
	return nil
}

func CampaignDailyLimit(val int) *errx.Error {
	if val < 3 || val > 100 {
		return errx.ErrCampaignDailyLimit
	}
	return nil
}

func CampaignStartDate(date time.Time) *errx.Error {
	if !date.After(time.Now()) {
		return errx.ErrCampaignStartDate
	}
	return nil
}

func CampaignEndDate(date time.Time) *errx.Error {
	if !date.After(time.Now()) {
		return errx.ErrCampaignEndDate
	}
	return nil
}

func CampaignDays(days uint8) *errx.Error {
	if err := bitmask.ValidateDaysMask(days); err != nil {
		return errx.ErrBitmask
	}
	return nil
}

func CampaignTime(input string) *errx.Error {
	_, err := time.Parse("15:04", input)
	if err != nil {
		return errx.ErrTime
	}
	return nil
}
