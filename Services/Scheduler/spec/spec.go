// Package spec defines the shape of a schedule and computes its fire times.
// The model only ever fills in a small JSON intent ("every weekday at
// 08:30"); this package turns that into concrete wall-clock times, so the
// recurrence maths stays deterministic and out of the model's hands.
//
// It is a leaf package: the Chat tool layer and the Scheduler engine both
// import it, and it imports neither, which keeps the dependency one-way.
package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Frequency values.
const (
	FreqDaily    = "daily"
	FreqWeekdays = "weekdays"
	FreqWeekly   = "weekly"
	FreqMonthly  = "monthly"
	FreqInterval = "interval"
)

// Schedule is the JSON stored in scheduled_tasks.schedule_json.
//
// At is the absolute fire time for a one-off reminder (unix ms, already
// resolved in the user's local zone by the tool layer). Freq/Time/Weekdays/
// Day/EveryMinutes describe a repeating routine; they are empty on a reminder.
type Schedule struct {
	At           int64  `json:"at,omitempty"`
	Freq         string `json:"freq,omitempty"`
	Time         string `json:"time,omitempty"`          // "HH:MM", local wall clock
	Weekdays     []int  `json:"weekdays,omitempty"`      // 0 = Sunday, weekly only
	Day          int    `json:"day,omitempty"`           // 1-31, monthly only; 0 = creation day
	EveryMinutes int    `json:"every_minutes,omitempty"` // interval only
}

// Encode/Decode keep the JSON shape in one place.
func Encode(s Schedule) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func Decode(raw string) (Schedule, error) {
	var s Schedule
	err := json.Unmarshal([]byte(raw), &s)
	return s, err
}

// Validate rejects anything the engine could not compute a next fire from.
func (s Schedule) Validate() error {
	if s.Freq == "" {
		if s.At <= 0 {
			return fmt.Errorf("a one-time schedule needs a future time")
		}
		return nil
	}
	if s.Freq != FreqInterval {
		if _, _, err := s.hourMinute(); err != nil {
			return err
		}
	}
	switch s.Freq {
	case FreqDaily:
		return nil
	case FreqMonthly:
		if s.Day < 0 || s.Day > 31 {
			return fmt.Errorf("monthly day %d is out of range (1-31)", s.Day)
		}
		return nil
	case FreqWeekdays:
		return nil
	case FreqWeekly:
		if len(s.Weekdays) == 0 {
			return fmt.Errorf("a weekly schedule needs at least one weekday")
		}
		for _, d := range s.Weekdays {
			if d < 0 || d > 6 {
				return fmt.Errorf("weekday %d is out of range (0 = Sunday)", d)
			}
		}
		return nil
	case FreqInterval:
		if s.EveryMinutes < 1 {
			return fmt.Errorf("an interval schedule needs every_minutes of at least 1")
		}
		return nil
	default:
		return fmt.Errorf("unknown frequency %q", s.Freq)
	}
}

func (s Schedule) hourMinute() (int, int, error) {
	parts := strings.Split(strings.TrimSpace(s.Time), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf(`time %q is not HH:MM`, s.Time)
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf(`time %q is not a valid HH:MM`, s.Time)
	}
	return h, m, nil
}

// NextAfter returns the first fire strictly after the instant afterMS.
// anchorMS is the moment the schedule was created (or its previous fire);
// interval steps from it and monthly keeps its day-of-month from it, so the
// maths stays stable across restarts. Computed in loc, i.e. the machine's
// wall clock — the user says "8am" and means their 8am.
func (s Schedule) NextAfter(afterMS int64, anchorMS int64, loc *time.Location) (int64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if s.Freq == "" {
		return s.At, nil
	}
	after := time.UnixMilli(afterMS)
	if s.Freq == FreqInterval {
		period := time.Duration(s.EveryMinutes) * time.Minute
		next := time.UnixMilli(anchorMS)
		for !next.After(after) {
			next = next.Add(period)
		}
		return next.UnixMilli(), nil
	}
	h, m, err := s.hourMinute()
	if err != nil {
		return 0, err
	}
	anchor := time.UnixMilli(anchorMS).In(loc)
	switch s.Freq {
	case FreqDaily, FreqWeekdays, FreqWeekly:
		for day := time.Duration(0); day <= 14*time.Hour*24; day += 24 * time.Hour {
			cand := dateAt(after.In(loc), day, h, m, loc)
			if !cand.After(after) {
				continue
			}
			switch s.Freq {
			case FreqDaily:
				return cand.UnixMilli(), nil
			case FreqWeekdays:
				if wd := cand.Weekday(); wd != time.Saturday && wd != time.Sunday {
					return cand.UnixMilli(), nil
				}
			case FreqWeekly:
				if weekdayIn(cand.Weekday(), s.Weekdays) {
					return cand.UnixMilli(), nil
				}
			}
		}
		return 0, fmt.Errorf("no upcoming fire found within two weeks")
	case FreqMonthly:
		d := s.Day
		if d == 0 {
			d = anchor.Day()
		}
		today := after.In(loc)
		y, mo := today.Year(), today.Month()
		for i := 0; i < 25; i++ {
			cand := clampDay(y, mo, d, h, m, loc)
			if cand.After(after) {
				return cand.UnixMilli(), nil
			}
			y, mo = addMonth(y, mo)
		}
		return 0, fmt.Errorf("no upcoming fire found within two years")
	}
	return 0, fmt.Errorf("unhandled frequency %q", s.Freq)
}

// SameAs reports whether two schedules would fire identically. The Chat tool
// layer uses it to refuse a second routine that re-states an existing one in
// different words — title and instruction text drift a little every time an
// agent rephrases, but the times a job runs do not.
func (s Schedule) SameAs(other Schedule) bool {
	if strings.ToLower(strings.TrimSpace(s.Freq)) != strings.ToLower(strings.TrimSpace(other.Freq)) {
		return false
	}
	if strings.TrimSpace(s.Time) != strings.TrimSpace(other.Time) {
		return false
	}
	if s.Day != other.Day || s.EveryMinutes != other.EveryMinutes {
		return false
	}
	if len(s.Weekdays) != len(other.Weekdays) {
		return false
	}
	a := append([]int{}, s.Weekdays...)
	b := append([]int{}, other.Weekdays...)
	sort.Ints(a)
	sort.Ints(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Window is how late a missed routine fire may still be worth running —
// the engine treats lateness beyond this as "that moment is gone" and
// silently advances. Reminders ignore this and always catch up.
func (s Schedule) Window() time.Duration {
	switch s.Freq {
	case FreqInterval:
		return 2 * time.Duration(s.EveryMinutes) * time.Minute
	case FreqWeekly:
		return 7 * 24 * time.Hour
	case FreqMonthly:
		return 31 * 24 * time.Hour
	default:
		// daily, weekdays, and one-off-at (unused for routines)
		return 24 * time.Hour
	}
}

// Summary is the human label the confirmation and list tools show.
func (s Schedule) Summary() string {
	if s.Freq == "" {
		return "once at " + time.UnixMilli(s.At).Format("Mon 2 Jan 2006 15:04")
	}
	switch s.Freq {
	case FreqDaily:
		return "every day at " + s.Time
	case FreqWeekdays:
		return "every weekday at " + s.Time
	case FreqWeekly:
		names := make([]string, 0, len(s.Weekdays))
		for _, d := range s.Weekdays {
			names = append(names, shortWeekday(d))
		}
		return "every " + strings.Join(names, ", ") + " at " + s.Time
	case FreqMonthly:
		d := s.Day
		if d == 0 {
			d = time.Now().Day()
		}
		return "monthly on day " + strconv.Itoa(d) + " at " + s.Time
	case FreqInterval:
		if s.EveryMinutes >= 1440 && s.EveryMinutes%1440 == 0 {
			return "every " + strconv.Itoa(s.EveryMinutes/1440) + " day(s)"
		}
		if s.EveryMinutes >= 60 && s.EveryMinutes%60 == 0 {
			return "every " + strconv.Itoa(s.EveryMinutes/60) + " hour(s)"
		}
		return "every " + strconv.Itoa(s.EveryMinutes) + " minute(s)"
	}
	return s.Freq
}

// ---- helpers ----

func dateAt(day time.Time, addDays time.Duration, h, m int, loc *time.Location) time.Time {
	y, mo, d := day.Date()
	return time.Date(y, mo, d, h, m, 0, 0, loc).Add(addDays)
}

func weekdayIn(wd time.Weekday, list []int) bool {
	for _, d := range list {
		if int(wd) == d {
			return true
		}
	}
	return false
}

// clampDay places h:m on day d of y-mo; months shorter than d fire on their
// last day (a "31st" routine still runs in February).
func clampDay(y int, mo time.Month, d, h, m int, loc *time.Location) time.Time {
	last := daysIn(y, mo)
	if d > last {
		d = last
	}
	return time.Date(y, mo, d, h, m, 0, 0, loc)
}

// daysIn is the last day of mo. time.Date normalizes day 0 to the last day
// of the previous month, so day 0 of mo+1 is mo's last day. (Verified on
// go1.26: Date(2026, December, 0) == 2026-11-30.)
func daysIn(y int, mo time.Month) int {
	return int(time.Date(y, mo+1, 0, 0, 0, 0, 0, time.UTC).Day())
}

func addMonth(y int, mo time.Month) (int, time.Month) {
	mo++
	if mo > 12 {
		mo = 1
		y++
	}
	return y, mo
}

func shortWeekday(d int) string {
	return time.Weekday(d).String()[:3]
}
