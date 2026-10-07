package models

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

const (
	WarmupReportBatchSize = 100
	WarmupReportMaxDays   = 366
)

type PoolLinkWarmupReportRequest struct {
	RemoteIDs []uuid.UUID `json:"remote_ids"`
	From      string      `json:"from"`
	To        string      `json:"to"`
}

func (r PoolLinkWarmupReportRequest) Range() (time.Time, time.Time, error) {
	if len(r.RemoteIDs) == 0 || len(r.RemoteIDs) > WarmupReportBatchSize {
		return time.Time{}, time.Time{}, fmt.Errorf("remote_ids must contain 1 to %d mailboxes", WarmupReportBatchSize)
	}
	period, err := ParseDayRange(r.From, r.To)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if period == nil || period.To.Sub(period.From).Hours()/24 >= WarmupReportMaxDays {
		return time.Time{}, time.Time{}, fmt.Errorf("a warmup report requires a range of at most %d days", WarmupReportMaxDays)
	}
	return period.From, period.To, nil
}

type WarmupPlacementDayRow struct {
	SenderID     uuid.UUID `json:"sender_id"`
	Email        string    `json:"email"`
	Date         string    `json:"date"`
	Group        string    `json:"group"`
	Inbox        int       `json:"inbox"`
	Tabs         int       `json:"tabs"`
	Spam         int       `json:"spam"`
	Rescued      int       `json:"rescued"`
	Unknown      int       `json:"unknown"`
	Archived     int       `json:"archived"`
	Custom       int       `json:"custom"`
	Instrumented int       `json:"instrumented_receipts"`
}

type WarmupPlacementHostRow struct {
	Group        string `json:"group"`
	Host         string `json:"host"`
	Inbox        int    `json:"inbox"`
	Tabs         int    `json:"tabs"`
	Spam         int    `json:"spam"`
	Rescued      int    `json:"rescued"`
	Unknown      int    `json:"unknown"`
	Archived     int    `json:"archived"`
	Custom       int    `json:"custom"`
	Instrumented int    `json:"instrumented_receipts"`
}

type WarmupSenderDayCount struct {
	SenderID uuid.UUID `json:"sender_id"`
	Email    string    `json:"email"`
	Date     string    `json:"date"`
	Count    int       `json:"count"`
}

// WarmupPlacementData carries counts, not rounded rates, so mixed pools are weighted correctly.
type WarmupPlacementData struct {
	Daily       []WarmupPlacementDayRow             `json:"daily"`
	Hosts       []WarmupPlacementHostRow            `json:"hosts"`
	Sent        []WarmupSenderDayCount              `json:"sent"`
	Unconfirmed []WarmupSenderDayCount              `json:"unconfirmed"`
	Windows     map[uuid.UUID]WarmupPlacementWindow `json:"windows"`
}

func MergeWarmupStats(local, cloud []WarmupDailyStats) []WarmupDailyStats {
	byDate := make(map[string]WarmupDailyStats, len(local)+len(cloud))
	for _, rows := range [][]WarmupDailyStats{local, cloud} {
		for _, row := range rows {
			day := byDate[row.Date]
			day.Date = row.Date
			day.EmailsSent += row.EmailsSent
			day.EmailsReplied += row.EmailsReplied
			day.EmailsReceived += row.EmailsReceived
			day.TargetVolume += row.TargetVolume
			day.Active = day.Active || row.Active
			byDate[row.Date] = day
		}
	}
	out := make([]WarmupDailyStats, 0, len(byDate))
	for _, day := range byDate {
		out = append(out, day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func (d *WarmupPlacementData) Add(other *WarmupPlacementData) {
	if other == nil {
		return
	}
	d.Daily = append(d.Daily, other.Daily...)
	d.Hosts = append(d.Hosts, other.Hosts...)
	d.Sent = append(d.Sent, other.Sent...)
	d.Unconfirmed = append(d.Unconfirmed, other.Unconfirmed...)
	if d.Windows == nil {
		d.Windows = make(map[uuid.UUID]WarmupPlacementWindow)
	}
	for id, window := range other.Windows {
		current := d.Windows[id]
		current.Add(window)
		d.Windows[id] = current
	}
}

func (d *WarmupPlacementData) RemapSenders(ids map[uuid.UUID]uuid.UUID) error {
	remap := func(id *uuid.UUID) error {
		target, ok := ids[*id]
		if !ok {
			return fmt.Errorf("unrequested warmup report sender")
		}
		*id = target
		return nil
	}
	for i := range d.Daily {
		if err := remap(&d.Daily[i].SenderID); err != nil {
			return err
		}
	}
	for _, rows := range [][]WarmupSenderDayCount{d.Sent, d.Unconfirmed} {
		for i := range rows {
			if err := remap(&rows[i].SenderID); err != nil {
				return err
			}
		}
	}
	windows := make(map[uuid.UUID]WarmupPlacementWindow, len(d.Windows))
	for id, window := range d.Windows {
		if err := remap(&id); err != nil {
			return err
		}
		current := windows[id]
		current.Add(window)
		windows[id] = current
	}
	d.Windows = windows
	return nil
}
