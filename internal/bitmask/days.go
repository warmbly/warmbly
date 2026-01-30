package bitmask

import "errors"

const validDaysMask uint8 = (1 << 7) - 1

func ValidateDaysMask(mask uint8) error {
	if mask == 0 {
		return errors.New("1 day needed minimum")
	}
	if mask & ^validDaysMask != 0 {
		return errors.New("invalid bit")
	}
	return nil
}

func DaysToMask(days []string) uint8 {
	var mask uint8 = 0
	mapping := map[string]uint8{
		"monday":    1 << 0,
		"tuesday":   1 << 1,
		"wednesday": 1 << 2,
		"thursday":  1 << 3,
		"friday":    1 << 4,
		"saturday":  1 << 5,
		"sunday":    1 << 6,
	}
	for _, d := range days {
		mask |= mapping[d]
	}
	return mask
}

func MaskToDays(mask uint8) []string {
	mapping := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	var days []string
	for i, day := range mapping {
		if mask&(1<<i) != 0 {
			days = append(days, day)
		}
	}
	return days
}

func DefaultDays() uint8 {
	return DaysToMask([]string{"monday", "tuesday", "wednesday", "thursday", "friday"})
}
