package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Keep IANA rules available in packaged desktops without a Go installation.
	"unicode/utf8"
)

var (
	ErrInvalidSchedule   = errors.New("invalid_schedule")
	ErrScheduleConflict  = errors.New("schedule_conflict")
	ErrScheduledTaskBusy = errors.New("scheduled_task_busy")
)

type TaskSchedule struct {
	Kind     string     `json:"kind"`
	Timezone string     `json:"timezone"`
	At       *time.Time `json:"at,omitempty"`
	Time     string     `json:"time,omitempty"`
	Weekdays []int      `json:"weekdays,omitempty"` // Sunday = 0
}

func (s TaskSchedule) Validate() error {
	if _, err := time.LoadLocation(s.Timezone); err != nil || s.Timezone == "" || s.Timezone == "Local" {
		return ErrInvalidSchedule
	}
	switch s.Kind {
	case "once":
		if s.At == nil || s.At.IsZero() || s.Time != "" || len(s.Weekdays) != 0 {
			return ErrInvalidSchedule
		}
	case "daily", "weekly":
		if s.At != nil {
			return ErrInvalidSchedule
		}
		if t, err := time.Parse("15:04", s.Time); err != nil || t.Format("15:04") != s.Time {
			return ErrInvalidSchedule
		}
		if s.Kind == "daily" && len(s.Weekdays) > 0 || s.Kind == "weekly" && len(s.Weekdays) == 0 {
			return ErrInvalidSchedule
		}
		seen := map[int]bool{}
		for _, d := range s.Weekdays {
			if d < 0 || d > 6 || seen[d] {
				return ErrInvalidSchedule
			}
			seen[d] = true
		}
	default:
		return ErrInvalidSchedule
	}
	return nil
}

// Next returns the first future occurrence. For an ambiguous local time only
// the earlier occurrence is eligible, even if after lies between the two.
func (s TaskSchedule) Next(after time.Time) *time.Time {
	if s.Validate() != nil {
		return nil
	}
	if s.Kind == "once" {
		if s.At.After(after) {
			v := s.At.UTC()
			return &v
		}
		return nil
	}
	loc, _ := time.LoadLocation(s.Timezone)
	clock, _ := time.Parse("15:04", s.Time)
	local := after.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, time.UTC)
	for n := 0; n < 16; n++ {
		d := day.AddDate(0, 0, n)
		if s.Kind == "weekly" && !slices.Contains(s.Weekdays, int(d.Weekday())) {
			continue
		}
		wall := time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)
		// Gather neighboring zone offsets, then verify the complete local date.
		// No time.Date normalization of nonexistent wall times is accepted.
		var earliest *time.Time
		offsets := map[int]bool{}
		for h := -36; h <= 36; h++ {
			_, off := wall.Add(time.Duration(h) * time.Hour).In(loc).Zone()
			offsets[off] = true
		}
		for off := range offsets {
			utc := wall.Add(-time.Duration(off) * time.Second)
			v := utc.In(loc)
			if v.Year() != d.Year() || v.Month() != d.Month() || v.Day() != d.Day() || v.Hour() != clock.Hour() || v.Minute() != clock.Minute() {
				continue
			}
			if earliest == nil || utc.Before(*earliest) {
				copy := utc
				earliest = &copy
			}
		}
		if earliest != nil && earliest.After(after) {
			return earliest
		}
	}
	return nil
}

type ScheduledTask struct {
	ID               string       `json:"id"`
	SessionID        string       `json:"sessionID"`
	Name             string       `json:"name"`
	Prompt           string       `json:"prompt"`
	Schedule         TaskSchedule `json:"schedule"`
	Enabled          bool         `json:"enabled"`
	Deleted          bool         `json:"deleted,omitempty"`
	Revision         int64        `json:"revision"`
	ScheduleRevision int64        `json:"scheduleRevision"`
	NextAt           *time.Time   `json:"nextAt,omitempty"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
	RequestID        string       `json:"-"`
	RequestHash      string       `json:"-"`
}

type ScheduledTaskCreate struct {
	SessionID    string       `json:"sessionID"`
	RequestID    string       `json:"requestID"`
	Name         string       `json:"name"`
	Prompt       string       `json:"prompt"`
	Schedule     TaskSchedule `json:"schedule"`
	DelaySeconds int64        `json:"delaySeconds,omitempty"`
}
type ScheduledTaskUpdate struct {
	Revision int64         `json:"revision"`
	Name     *string       `json:"name,omitempty"`
	Prompt   *string       `json:"prompt,omitempty"`
	Schedule *TaskSchedule `json:"schedule,omitempty"`
	Enabled  *bool         `json:"enabled,omitempty"`
	Delete   bool          `json:"delete,omitempty"`
}

func PrepareScheduledTask(in ScheduledTaskCreate, now time.Time) (*ScheduledTask, error) {
	in.Name, in.Prompt = strings.TrimSpace(in.Name), strings.TrimSpace(in.Prompt)
	if in.SessionID == "" || in.RequestID == "" || len(in.RequestID) > 256 || !validTaskText(in.Name, in.Prompt) {
		return nil, ErrInvalidSchedule
	}
	raw, _ := json.Marshal(in)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if in.DelaySeconds != 0 {
		if in.DelaySeconds < 1 || in.DelaySeconds > 366*86400 || in.Schedule.Kind != "once" || in.Schedule.At != nil {
			return nil, ErrInvalidSchedule
		}
		at := now.Add(time.Duration(in.DelaySeconds) * time.Second).UTC().Truncate(time.Millisecond)
		in.Schedule.At = &at
	}
	if err := in.Schedule.Validate(); err != nil {
		return nil, err
	}
	next := in.Schedule.Next(now)
	return &ScheduledTask{ID: NewID("schedule"), SessionID: in.SessionID, Name: in.Name, Prompt: in.Prompt, Schedule: in.Schedule, Enabled: true, Revision: 1, ScheduleRevision: 1, NextAt: next, CreatedAt: now, UpdatedAt: now, RequestID: in.RequestID, RequestHash: hash}, nil
}
func validTaskText(name, prompt string) bool {
	return name != "" && utf8.RuneCountInString(name) <= 100 && prompt != "" && len(prompt) <= 65536
}
func ApplyScheduledTaskUpdate(t *ScheduledTask, in ScheduledTaskUpdate, now time.Time) error {
	if t.Deleted {
		return ErrNotFound
	}
	if t.Revision != in.Revision {
		return ErrScheduleConflict
	}
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if in.Prompt != nil {
		t.Prompt = strings.TrimSpace(*in.Prompt)
	}
	if !validTaskText(t.Name, t.Prompt) {
		return ErrInvalidSchedule
	}
	if in.Schedule != nil {
		if err := in.Schedule.Validate(); err != nil {
			return err
		}
		t.Schedule = *in.Schedule
		t.ScheduleRevision++
		t.NextAt = t.Schedule.Next(now)
		if t.NextAt == nil {
			return ErrInvalidSchedule
		}
	}
	if in.Enabled != nil {
		if *in.Enabled && !t.Enabled {
			if t.Schedule.Kind == "once" && t.NextAt == nil && in.Schedule == nil {
				return ErrInvalidSchedule
			}
			t.NextAt = t.Schedule.Next(now)
			if t.NextAt == nil {
				return ErrInvalidSchedule
			}
		}
		t.Enabled = *in.Enabled
	}
	if in.Delete {
		t.Deleted = true
		t.Enabled = false
	}
	t.Revision++
	t.UpdatedAt = now
	return nil
}

type ScheduledTaskRun struct {
	Schedule           TaskSchedule `json:"schedule"`
	ID                 string       `json:"id"`
	TaskID             string       `json:"taskID"`
	SessionID          string       `json:"sessionID"`
	Name               string       `json:"name"`
	Prompt             string       `json:"prompt"`
	DefinitionRevision int64        `json:"definitionRevision"`
	Source             string       `json:"source"`
	ScheduledFor       time.Time    `json:"scheduledFor"`
	AcceptedAt         time.Time    `json:"acceptedAt"`
	ClientMessageID    string       `json:"clientMessageID"`
	Handoff            string       `json:"handoff"` // pending, submitted, skipped, failed; never a copy of turn status
	Reason             string       `json:"reason,omitempty"`
	SkippedThrough     *time.Time   `json:"skippedThrough,omitempty"`
	Key                string       `json:"-"`
}
type ScheduledTaskAccept struct {
	Revision   int64
	RequestID  string // manual only
	Now        time.Time
	SkipReason string
}

func PrepareScheduledTaskRun(t *ScheduledTask, in ScheduledTaskAccept) (*ScheduledTaskRun, error) {
	if t.Deleted {
		return nil, ErrNotFound
	}
	if t.Revision != in.Revision {
		return nil, ErrScheduleConflict
	}
	source, key, at := "manual", "manual:"+in.RequestID, in.Now
	if in.RequestID == "" {
		if !t.Enabled || t.NextAt == nil || t.NextAt.After(in.Now) {
			return nil, ErrScheduleConflict
		}
		source = "scheduled"
		at = *t.NextAt
		key = fmt.Sprintf("scheduled:%d:%d", t.ScheduleRevision, at.UnixMilli())
	}
	r := &ScheduledTaskRun{ID: NewID("scheduled_run"), TaskID: t.ID, SessionID: t.SessionID, Name: t.Name, Prompt: t.Prompt, Schedule: t.Schedule, DefinitionRevision: t.Revision, Source: source, ScheduledFor: at, AcceptedAt: in.Now, Handoff: "pending", Key: key}
	r.ClientMessageID = r.ID
	if in.SkipReason != "" {
		r.Handoff = "skipped"
		r.Reason = in.SkipReason
		if in.SkipReason == "missed" {
			v := in.Now
			r.SkippedThrough = &v
		}
	}
	if t.Schedule.Kind == "once" {
		t.NextAt = nil
	} else if source == "scheduled" {
		t.NextAt = t.Schedule.Next(in.Now)
	}
	t.Revision++
	t.UpdatedAt = in.Now
	return r, nil
}

type ScheduledTaskStore interface {
	GetScheduledTaskTurn(context.Context, string, string) (*ConversationTurn, error)
	GetScheduledTaskRun(context.Context, string, string) (*ScheduledTaskRun, error)
	CreateScheduledTask(context.Context, *ScheduledTask) (*ScheduledTask, error)
	GetScheduledTask(context.Context, string) (*ScheduledTask, error)
	ListScheduledTasks(context.Context, string, bool) ([]*ScheduledTask, error)
	UpdateScheduledTask(context.Context, string, ScheduledTaskUpdate, time.Time) (*ScheduledTask, error)
	AcceptScheduledTask(context.Context, string, ScheduledTaskAccept) (*ScheduledTaskRun, error)
	ListScheduledTaskRuns(context.Context, string, int, int) ([]*ScheduledTaskRun, error)
	PendingScheduledTaskRuns(context.Context) ([]*ScheduledTaskRun, error)
	SetScheduledTaskHandoff(context.Context, string, string, string) error
	FindQueuedInput(context.Context, string, string) (*QueuedInput, error)
	FindInputTurn(context.Context, string, string) (*Turn, error)
	FindRetryTurn(context.Context, string, string) (*Turn, error)
}
