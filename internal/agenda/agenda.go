package agenda

import (
	"sort"
	"time"

	"orgzd/internal/org"
)

type Entry struct {
	*org.Entry
	Date       time.Time
	HasTime    bool
	IsDeadline bool
}

func (e Entry) DateLabel() string {
	return e.Date.Format("02 Jan")
}

func (e Entry) TimeLabel() string {
	if e.HasTime {
		return e.Date.Format("15:04")
	}
	return ""
}

type Group struct {
	Label   string
	Type    string // overdue, today, tomorrow, week, later
	Entries []Entry
}

func Build(entries []*org.Entry, now time.Time) []Group {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := today.AddDate(0, 0, 1)
	weekEnd := today.AddDate(0, 0, 7)

	buckets := map[string]*[]Entry{
		"overdue":  {},
		"today":    {},
		"tomorrow": {},
		"week":     {},
		"later":    {},
	}

	for _, e := range entries {
		if e.IsDone {
			continue
		}
		var ts *org.Timestamp
		var isDl bool
		if e.Scheduled != nil {
			ts = e.Scheduled
		} else if e.Deadline != nil {
			ts = e.Deadline
			isDl = true
		} else {
			continue
		}

		ae := Entry{Entry: e, Date: ts.Time, HasTime: ts.HasTime, IsDeadline: isDl}
		d := time.Date(ts.Time.Year(), ts.Time.Month(), ts.Time.Day(), 0, 0, 0, 0, now.Location())

		var bucket *[]Entry
		switch {
		case d.Before(today):
			bucket = buckets["overdue"]
		case d.Equal(today):
			bucket = buckets["today"]
		case d.Equal(tomorrow):
			bucket = buckets["tomorrow"]
		case d.Before(weekEnd):
			bucket = buckets["week"]
		default:
			bucket = buckets["later"]
		}
		*bucket = append(*bucket, ae)
	}

	for _, b := range buckets {
		sort.SliceStable(*b, func(i, j int) bool {
			a, c := (*b)[i], (*b)[j]
			if !a.Date.Equal(c.Date) {
				return a.Date.Before(c.Date)
			}
			if a.File != c.File {
				return a.File < c.File
			}
			return a.Line < c.Line
		})
	}

	var groups []Group
	if len(*buckets["overdue"]) > 0 {
		groups = append(groups, Group{Label: "Overdue", Type: "overdue", Entries: *buckets["overdue"]})
	}
	groups = append(groups, Group{
		Label:   "Today \u2014 " + today.Format("Mon 02 Jan"),
		Type:    "today",
		Entries: *buckets["today"],
	})
	if len(*buckets["tomorrow"]) > 0 {
		groups = append(groups, Group{
			Label:   "Tomorrow \u2014 " + tomorrow.Format("Mon 02 Jan"),
			Type:    "tomorrow",
			Entries: *buckets["tomorrow"],
		})
	}
	if len(*buckets["week"]) > 0 {
		groups = append(groups, Group{Label: "This week", Type: "week", Entries: *buckets["week"]})
	}
	if len(*buckets["later"]) > 0 {
		groups = append(groups, Group{Label: "Later", Type: "later", Entries: *buckets["later"]})
	}
	return groups
}
