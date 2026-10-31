package fleet

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

var dayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Schedule is "these weekdays at HH:MM in this timezone".
type Schedule struct {
	Days []string // subset of dayNames
	Time string   // HH:MM
	Loc  *time.Location
}

func parseSchedule(days, hhmm, tz string) (Schedule, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Schedule{}, invalid("timezone", "unknown timezone %q", tz)
	}
	var ds []string
	for _, d := range strings.Split(days, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if !slices.Contains(dayNames, d) {
			return Schedule{}, invalid("days", "unknown day %q; use sun, mon, tue, wed, thu, fri, sat", d)
		}
		if !slices.Contains(ds, d) {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 {
		return Schedule{}, invalid("days", "pick at least one day")
	}
	if _, _, err := parseHHMM(hhmm); err != nil {
		return Schedule{}, err
	}
	slices.SortFunc(ds, func(a, b string) int { return slices.Index(dayNames, a) - slices.Index(dayNames, b) })
	return Schedule{Days: ds, Time: hhmm, Loc: loc}, nil
}

func parseHHMM(s string) (int, int, error) {
	var h, m int
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, invalid("time", "use HH:MM, for example 03:30")
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil || h > 23 || m > 59 {
		return 0, 0, invalid("time", "use HH:MM, for example 03:30")
	}
	return h, m, nil
}

func (sc Schedule) slotOn(day time.Time) time.Time {
	h, m, _ := parseHHMM(sc.Time)
	y, mo, d := day.Date()
	return time.Date(y, mo, d, h, m, 0, 0, sc.Loc)
}

func (sc Schedule) runsOn(t time.Time) bool { return slices.Contains(sc.Days, dayNames[t.Weekday()]) }

// Latest returns the most recent slot at or before now.
func (sc Schedule) Latest(now time.Time) time.Time {
	local := now.In(sc.Loc)
	for i := 0; i <= 7; i++ {
		day := local.AddDate(0, 0, -i)
		if !sc.runsOn(day) {
			continue
		}
		if slot := sc.slotOn(day); !slot.After(now) {
			return slot.UTC()
		}
	}
	return time.Time{}
}

// Next returns the first slot after now.
func (sc Schedule) Next(now time.Time) time.Time {
	local := now.In(sc.Loc)
	for i := 0; i <= 7; i++ {
		day := local.AddDate(0, 0, i)
		if !sc.runsOn(day) {
			continue
		}
		if slot := sc.slotOn(day); slot.After(now) {
			return slot.UTC()
		}
	}
	return time.Time{}
}
