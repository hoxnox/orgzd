package org

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Timestamp struct {
	Time     time.Time
	HasTime  bool
	Repeater string
}

type Entry struct {
	Level     int
	State     string
	IsDone    bool
	Title     string
	Tags      []string
	Scheduled *Timestamp
	Deadline  *Timestamp
	Closed    *Timestamp
	Body      string
	File      string
	Line      int
}

func (e *Entry) RelevantDate() *Timestamp {
	if e.Closed != nil {
		return e.Closed
	}
	if e.Scheduled != nil {
		return e.Scheduled
	}
	return e.Deadline
}

func (ts *Timestamp) String() string {
	if ts == nil {
		return ""
	}
	f := "Mon 02 Jan 2006"
	if ts.HasTime {
		f = "Mon 02 Jan 2006 15:04"
	}
	s := ts.Time.Format(f)
	if ts.Repeater != "" {
		s += " " + ts.Repeater
	}
	return s
}

var (
	todoDefRe           = regexp.MustCompile(`^#\+TODO:\s*(.+)$`)
	headlineRe          = regexp.MustCompile(`^(\*+)\s+(.+)$`)
	timestampRe         = regexp.MustCompile(`<(\d{4}-\d{2}-\d{2})(?:\s+[A-Za-z]{2,3})?(?:\s+(\d{1,2}:\d{2}))?(?:\s+([.+]+\d+[hdwmy]))?>`)
	inactiveTimestampRe = regexp.MustCompile(`\[(\d{4}-\d{2}-\d{2})(?:\s+[A-Za-z]{2,3})?(?:\s+(\d{1,2}:\d{2}))?\]`)
	tagsRe              = regexp.MustCompile(`\s+:([\w@:]+):\s*$`)
	drawerOpenRe        = regexp.MustCompile(`^\s*:[A-Z][A-Z_]*:\s*$`)
)

func ParseDir(dir string) ([]*Entry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.org"))
	if err != nil {
		return nil, err
	}
	var all []*Entry
	for _, f := range files {
		if IsSyncConflict(filepath.Base(f)) {
			continue
		}
		entries, err := ParseFile(f)
		if err != nil {
			log.Printf("warning: %s: %v", f, err)
			continue
		}
		all = append(all, entries...)
	}
	return all, nil
}

func ParseFile(path string) ([]*Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	activeStates := []string{"TODO", "WAIT"}
	doneStates := []string{"DONE", "CANCELED"}
	statesMap := makeSet(append(activeStates, doneStates...))
	doneMap := makeSet(doneStates)

	scanner := bufio.NewScanner(f)
	var entries []*Entry
	var current *Entry
	var bodyLines []string
	inPlanning := false
	inDrawer := false
	lineNum := 0
	fname := filepath.Base(path)

	finalize := func() {
		if current != nil {
			current.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
			bodyLines = nil
		}
	}

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if m := todoDefRe.FindStringSubmatch(line); m != nil {
			activeStates, doneStates = parseTodoDef(m[1])
			statesMap = makeSet(append(activeStates, doneStates...))
			doneMap = makeSet(doneStates)
			continue
		}

		if m := headlineRe.FindStringSubmatch(line); m != nil {
			finalize()
			level := len(m[1])
			rest := m[2]

			var state string
			if idx := strings.IndexByte(rest, ' '); idx > 0 {
				word := rest[:idx]
				if statesMap[word] {
					state = word
					rest = rest[idx+1:]
				}
			} else if statesMap[rest] {
				state = rest
				rest = ""
			}

			var tags []string
			if tm := tagsRe.FindStringSubmatch(rest); tm != nil {
				for _, t := range strings.Split(tm[1], ":") {
					if t != "" {
						tags = append(tags, t)
					}
				}
				rest = rest[:len(rest)-len(tm[0])]
			}

			current = &Entry{
				Level:  level,
				State:  state,
				IsDone: doneMap[state],
				Title:  strings.TrimSpace(rest),
				Tags:   tags,
				File:   fname,
				Line:   lineNum,
			}
			inPlanning = true
			inDrawer = false
			entries = append(entries, current)
			continue
		}

		if current == nil {
			continue
		}

		trimmed := strings.TrimSpace(line)

		if inDrawer {
			if trimmed == ":END:" {
				inDrawer = false
			}
			continue
		}

		if drawerOpenRe.MatchString(line) {
			inDrawer = true
			continue
		}

		if inPlanning {
			if trimmed == "" {
				inPlanning = false
				continue
			}
			found := false
			if strings.Contains(line, "SCHEDULED:") {
				if ts := parseTimestamp(line[strings.Index(line, "SCHEDULED:"):]); ts != nil {
					current.Scheduled = ts
					found = true
				}
			}
			if strings.Contains(line, "DEADLINE:") {
				if ts := parseTimestamp(line[strings.Index(line, "DEADLINE:"):]); ts != nil {
					current.Deadline = ts
					found = true
				}
			}
			if strings.Contains(line, "CLOSED:") {
				if ts := parseInactiveTimestamp(line[strings.Index(line, "CLOSED:"):]); ts != nil {
					current.Closed = ts
				}
				found = true
			}
			if !found {
				inPlanning = false
				bodyLines = append(bodyLines, line)
			}
			continue
		}

		bodyLines = append(bodyLines, line)
	}
	finalize()
	return entries, scanner.Err()
}

func parseTimestamp(s string) *Timestamp {
	m := timestampRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	t, hasTime, err := parseDateTime(m[1], m[2])
	if err != nil {
		return nil
	}
	return &Timestamp{Time: t, HasTime: hasTime, Repeater: m[3]}
}

func parseInactiveTimestamp(s string) *Timestamp {
	m := inactiveTimestampRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	t, hasTime, err := parseDateTime(m[1], m[2])
	if err != nil {
		return nil
	}
	return &Timestamp{Time: t, HasTime: hasTime}
}

func parseDateTime(date, hms string) (time.Time, bool, error) {
	if hms == "" {
		t, err := time.ParseInLocation("2006-01-02", date, time.Local)
		return t, false, err
	}
	if strings.Index(hms, ":") == 1 {
		hms = "0" + hms
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hms, time.Local)
	return t, true, err
}

func parseTodoDef(def string) (active, done []string) {
	parts := strings.SplitN(def, "|", 2)
	for _, kw := range strings.Fields(parts[0]) {
		active = append(active, kw)
	}
	if len(parts) > 1 {
		for _, kw := range strings.Fields(parts[1]) {
			done = append(done, kw)
		}
	}
	return
}

func makeSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
