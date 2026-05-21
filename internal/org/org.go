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
	File      string
	Line      int
}

var (
	todoDefRe   = regexp.MustCompile(`^#\+TODO:\s*(.+)$`)
	headlineRe  = regexp.MustCompile(`^(\*+)\s+(.+)$`)
	timestampRe = regexp.MustCompile(`<(\d{4}-\d{2}-\d{2})(?:\s+[A-Za-z]{2,3})?(?:\s+(\d{2}:\d{2}))?(?:\s+([.+]+\d+[hdwmy]))?>`)
	tagsRe      = regexp.MustCompile(`\s+:([\w@:]+):\s*$`)
	propLineRe  = regexp.MustCompile(`^\s*:[\w-]+:`)
)

func ParseDir(dir string) ([]*Entry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.org"))
	if err != nil {
		return nil, err
	}
	var all []*Entry
	for _, f := range files {
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
	inPlanning := false
	lineNum := 0
	fname := filepath.Base(path)

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
			entries = append(entries, current)
			continue
		}

		if current == nil || !inPlanning {
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inPlanning = false
			continue
		}
		if propLineRe.MatchString(trimmed) {
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
			found = true
		}
		if !found {
			inPlanning = false
		}
	}
	return entries, scanner.Err()
}

func parseTimestamp(s string) *Timestamp {
	m := timestampRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	var t time.Time
	var hasTime bool
	var err error
	if m[2] != "" {
		t, err = time.ParseInLocation("2006-01-02 15:04", m[1]+" "+m[2], time.Local)
		hasTime = true
	} else {
		t, err = time.ParseInLocation("2006-01-02", m[1], time.Local)
	}
	if err != nil {
		return nil
	}
	return &Timestamp{Time: t, HasTime: hasTime, Repeater: m[3]}
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
