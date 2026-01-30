package tz

import (
	"slices"
	"time"
)

var Times []string = make([]string, 0)

func fetchTimes() {
	var options []string
	for h := range 24 {
		for m := 0; m < 60; m += 30 {
			t := time.Date(0, 1, 1, h, m, 0, 0, time.UTC)
			options = append(options, t.Format("03:04 PM"))
		}
	}
	Times = options
}

func ValidTime(t string) bool {
	return slices.Contains(Times, t)
}
