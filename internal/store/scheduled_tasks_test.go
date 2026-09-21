package store

import (
	"testing"
	"time"
)

func TestTaskScheduleCalendar(t *testing.T) {
	for _, tc := range []struct {
		name, zone, clock, after, want string
		days                           []int
	}{
		{"daily", "Asia/Shanghai", "09:00", "2026-09-20T01:00:00Z", "2026-09-21T01:00:00Z", nil},
		{"weekly", "Asia/Singapore", "09:00", "2026-09-21T01:00:00Z", "2026-09-28T01:00:00Z", []int{1}},
		{"spring gap", "America/New_York", "02:30", "2026-03-08T05:00:00Z", "2026-03-09T06:30:00Z", nil},
		{"autumn first", "America/New_York", "01:30", "2026-11-01T04:00:00Z", "2026-11-01T05:30:00Z", nil},
		{"autumn no repeat", "America/New_York", "01:30", "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z", nil},
		{"half hour gap", "Australia/Lord_Howe", "02:15", "2026-10-03T13:00:00Z", "2026-10-04T15:15:00Z", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := "daily"
			if len(tc.days) > 0 {
				kind = "weekly"
			}
			schedule := TaskSchedule{Kind: kind, Timezone: tc.zone, Time: tc.clock, Weekdays: tc.days}
			after, _ := time.Parse(time.RFC3339, tc.after)
			got := schedule.Next(after)
			if got == nil || got.Format(time.RFC3339) != tc.want {
				t.Fatalf("next=%v want=%s", got, tc.want)
			}
		})
	}
}

func TestTaskScheduleRejectInvalid(t *testing.T) {
	for _, s := range []TaskSchedule{
		{Kind: "daily", Timezone: "Local", Time: "09:00"},
		{Kind: "daily", Timezone: "UTC", Time: "9:00"},
		{Kind: "weekly", Timezone: "UTC", Time: "09:00"},
		{Kind: "weekly", Timezone: "UTC", Time: "09:00", Weekdays: []int{1, 1}},
		{Kind: "once", Timezone: "UTC"},
	} {
		if s.Validate() == nil {
			t.Fatalf("accepted invalid schedule: %+v", s)
		}
	}
}
