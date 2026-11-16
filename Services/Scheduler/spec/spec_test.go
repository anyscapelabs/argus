package spec

import (
	"testing"
	"time"
)

var utc = time.UTC

func ms(y int, mo time.Month, d, h, mi int) int64 {
	return time.Date(y, mo, d, h, mi, 0, 0, utc).UnixMilli()
}

func TestNextAfterDailyRollsToTomorrow(t *testing.T) {
	s := Schedule{Freq: FreqDaily, Time: "08:00"}
	// same day 07:00 -> today 08:00
	got, err := s.NextAfter(ms(2026, time.November, 3, 7, 0), ms(2026, time.November, 1, 8, 0), utc)
	if err != nil {
		t.Fatal(err)
	}
	if want := ms(2026, time.November, 3, 8, 0); got != want {
		t.Fatalf("got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// same day 08:00 sharp -> tomorrow (strictly after)
	got, _ = s.NextAfter(ms(2026, time.November, 3, 8, 0), ms(2026, time.November, 1, 8, 0), utc)
	if want := ms(2026, time.November, 4, 8, 0); got != want {
		t.Fatalf("boundary: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// after 08:00 -> tomorrow
	got, _ = s.NextAfter(ms(2026, time.November, 3, 9, 30), ms(2026, time.November, 1, 8, 0), utc)
	if want := ms(2026, time.November, 4, 8, 0); got != want {
		t.Fatalf("rollover: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
}

func TestNextAfterWeekdaysSkipsWeekend(t *testing.T) {
	s := Schedule{Freq: FreqWeekdays, Time: "09:00"}
	// Friday 2026-11-06 10:00 (past 09:00) -> Monday 2026-11-09
	got, err := s.NextAfter(ms(2026, time.November, 6, 10, 0), ms(2026, time.November, 2, 9, 0), utc)
	if err != nil {
		t.Fatal(err)
	}
	if want := ms(2026, time.November, 9, 9, 0); got != want {
		t.Fatalf("got %s (%s), want %s", time.UnixMilli(got).Weekday(), time.UnixMilli(got), time.UnixMilli(want))
	}
	// Friday 08:00 -> Friday 09:00 same day
	got, _ = s.NextAfter(ms(2026, time.November, 6, 8, 0), ms(2026, time.November, 2, 9, 0), utc)
	if want := ms(2026, time.November, 6, 9, 0); got != want {
		t.Fatalf("same-day: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
}

func TestNextAfterWeekly(t *testing.T) {
	// Mon + Fri, now Tue 2026-11-03 00:00 -> Fri 2026-11-06 08:30
	s := Schedule{Freq: FreqWeekly, Time: "08:30", Weekdays: []int{1, 5}}
	got, err := s.NextAfter(ms(2026, time.November, 3, 0, 0), ms(2026, time.October, 30, 8, 30), utc)
	if err != nil {
		t.Fatal(err)
	}
	if want := ms(2026, time.November, 6, 8, 30); got != want {
		t.Fatalf("got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// Fri 09:00 -> Mon 08:30
	got, _ = s.NextAfter(ms(2026, time.November, 6, 9, 0), ms(2026, time.October, 30, 8, 30), utc)
	if want := ms(2026, time.November, 9, 8, 30); got != want {
		t.Fatalf("wrap: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
}

func TestNextAfterMonthlyKeepsDayAndClamps(t *testing.T) {
	// day 31 in a 31-day month: Nov 3 -> Dec 31 would clamp (Nov has 30 days)
	s := Schedule{Freq: FreqMonthly, Time: "10:00", Day: 31}
	got, err := s.NextAfter(ms(2026, time.November, 3, 0, 0), ms(2026, time.October, 31, 10, 0), utc)
	if err != nil {
		t.Fatal(err)
	}
	if want := ms(2026, time.November, 30, 10, 0); got != want {
		t.Fatalf("november clamp: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// past Nov 30 -> Dec 31 really exists, so the full day is used
	got, _ = s.NextAfter(ms(2026, time.November, 30, 11, 0), ms(2026, time.October, 31, 10, 0), utc)
	if want := ms(2026, time.December, 31, 10, 0); got != want {
		t.Fatalf("december: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// past Dec 31 -> Jan 31 (still a 31-day month)
	got, _ = s.NextAfter(ms(2026, time.December, 31, 11, 0), ms(2026, time.October, 31, 10, 0), utc)
	if want := ms(2027, time.January, 31, 10, 0); got != want {
		t.Fatalf("january: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// past Jan 31 -> Feb clamps to 28 (2027 is not a leap year)
	got, _ = s.NextAfter(ms(2027, time.January, 31, 11, 0), ms(2026, time.October, 31, 10, 0), utc)
	if want := ms(2027, time.February, 28, 10, 0); got != want {
		t.Fatalf("february clamp: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// Day 0 falls back to the anchor's day-of-month
	s2 := Schedule{Freq: FreqMonthly, Time: "10:00"}
	got, _ = s2.NextAfter(ms(2026, time.November, 3, 0, 0), ms(2026, time.October, 15, 10, 0), utc)
	if want := ms(2026, time.November, 15, 10, 0); got != want {
		t.Fatalf("anchor fallback: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
}

func TestNextAfterIntervalStepsFromAnchor(t *testing.T) {
	s := Schedule{Freq: FreqInterval, EveryMinutes: 90}
	anchor := ms(2026, time.November, 3, 0, 0)
	// well past several periods: 5h later -> anchor + 4*90m = 06:00
	got, err := s.NextAfter(ms(2026, time.November, 3, 5, 0), anchor, utc)
	if err != nil {
		t.Fatal(err)
	}
	if want := ms(2026, time.November, 3, 6, 0); got != want {
		t.Fatalf("got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
	// exactly on a step -> next step (strictly after)
	got, _ = s.NextAfter(ms(2026, time.November, 3, 3, 0), anchor, utc)
	if want := ms(2026, time.November, 3, 4, 30); got != want {
		t.Fatalf("boundary: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
}

func TestOneOffReturnsAt(t *testing.T) {
	at := ms(2026, time.November, 5, 9, 0)
	s := Schedule{At: at}
	got, err := s.NextAfter(ms(2026, time.November, 3, 0, 0), ms(2026, time.November, 3, 0, 0), utc)
	if err != nil || got != at {
		t.Fatalf("got %d/%v, want %d", got, err, at)
	}
}

func TestValidateRejectsJunk(t *testing.T) {
	if (Schedule{}).Validate() == nil {
		t.Fatal("empty schedule accepted")
	}
	if (Schedule{Freq: FreqDaily, Time: "25:00"}).Validate() == nil {
		t.Fatal("bad time accepted")
	}
	if (Schedule{Freq: FreqWeekly, Time: "08:00"}).Validate() == nil {
		t.Fatal("weekly without weekdays accepted")
	}
	if (Schedule{Freq: FreqWeekly, Time: "08:00", Weekdays: []int{7}}).Validate() == nil {
		t.Fatal("weekday 7 accepted")
	}
	if (Schedule{Freq: FreqInterval}).Validate() == nil {
		t.Fatal("interval without every_minutes accepted")
	}
	if err := (Schedule{Freq: FreqMonthly, Time: "08:00", Day: 31}).Validate(); err != nil {
		t.Fatalf("valid monthly rejected: %v", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	s := Schedule{Freq: FreqWeekly, Time: "08:30", Weekdays: []int{1, 5}, Day: 3, At: 12345, EveryMinutes: 45}
	raw, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if again != raw {
		t.Fatalf("round trip lost data: %s vs %s", again, raw)
	}
}
