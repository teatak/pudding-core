package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestScheduledRunsPreserveDefinitionRevision(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	for _, mode := range []string{"scheduled", "manual", "missed", "busy"} {
		t.Run(mode, func(t *testing.T) {
			task, err := PrepareScheduledTask(ScheduledTaskCreate{SessionID: "s", RequestID: "create", Name: "Check", Prompt: "Check project", Schedule: TaskSchedule{Kind: "daily", Timezone: "UTC", Time: "09:00"}}, now)
			if err != nil {
				t.Fatal(err)
			}
			original := *task
			accept := ScheduledTaskAccept{Revision: task.Revision, ExpectedNextAt: task.NextAt, Now: *task.NextAt}
			if mode == "manual" {
				accept.RequestID = "manual"
			}
			if mode == "missed" || mode == "busy" {
				accept.SkipReason = mode
			}
			run, err := PrepareScheduledTaskRun(task, accept)
			if err != nil {
				t.Fatal(err)
			}
			if task.Revision != original.Revision || !task.UpdatedAt.Equal(original.UpdatedAt) || run.DefinitionRevision != original.Revision {
				t.Fatalf("run changed definition: task=%+v run=%+v", task, run)
			}
			if mode != "manual" {
				if !task.NextAt.After(*original.NextAt) {
					t.Fatal("automatic run did not advance due time")
				}
				// Even once the next occurrence becomes due, an old admission snapshot
				// must not consume that different occurrence.
				accept.Now = *task.NextAt
				before := *task
				if _, err := PrepareScheduledTaskRun(task, accept); !errors.Is(err, ErrScheduleConflict) || !reflect.DeepEqual(before, *task) {
					t.Fatalf("stale occurrence accepted or mutated task: %v", err)
				}
			} else if !task.NextAt.Equal(*original.NextAt) {
				t.Fatal("manual run moved recurring due time")
			}
			name := "Edited"
			if err := ApplyScheduledTaskUpdate(task, ScheduledTaskUpdate{Revision: original.Revision, Name: &name}, accept.Now); err != nil {
				t.Fatalf("execution caused an edit conflict: %v", err)
			}
			var conflict *ScheduleConflictError
			err = ApplyScheduledTaskUpdate(task, ScheduledTaskUpdate{Revision: original.Revision, Delete: true}, accept.Now)
			if !errors.Is(err, ErrScheduleConflict) || !errors.As(err, &conflict) || conflict.Current.Name != name || conflict.Current.Revision != original.Revision+1 || task.Deleted {
				t.Fatalf("stale edit lost conflict snapshot: %+v %v", conflict, err)
			}
		})
	}
}

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
