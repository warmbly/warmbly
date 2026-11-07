// Package idlepoll paces a loop that polls a Postgres queue: it asks again at
// once while work keeps coming and slows down while the queue stays empty.
package idlepoll

import "time"

// Pace is a poll interval that doubles from Min to Max while polls find nothing.
type Pace struct {
	Min, Max time.Duration
	cur      time.Duration
}

// Next is the wait before the next poll. A full poll means more may be
// waiting, so it is asked for at once; any work resets the pace to Min.
func (p *Pace) Next(found int, full bool) time.Duration {
	switch {
	case full:
		p.cur = p.Min
		return 0
	case found > 0 || p.cur == 0:
		p.cur = p.Min
	default:
		p.cur = min(p.cur*2, p.Max)
	}
	return p.cur
}
