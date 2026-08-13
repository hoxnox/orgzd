package org

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func MarkDone(dir, file string, line int, now time.Time) error {
	path := filepath.Join(dir, file)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	idx := line - 1
	if idx < 0 || idx >= len(lines) {
		return fmt.Errorf("line %d out of range", line)
	}
	if !headlineRe.MatchString(lines[idx]) {
		return fmt.Errorf("line %d is not a headline", line)
	}

	_, doneStates := fileTodoDef(lines)
	allStates := makeSet(append(doneStates, func() []string {
		a, _ := fileTodoDef(lines)
		return a
	}()...))
	doneState := "DONE"
	if len(doneStates) > 0 {
		doneState = doneStates[0]
	}

	schedIdx, schedTS := findScheduled(lines, idx)
	isRepeating := schedTS != nil && schedTS.Repeater != ""

	if isRepeating {
		newDate := advanceDate(schedTS, now)
		newTS := formatActiveTS(newDate, schedTS.HasTime, schedTS.Repeater)
		lines[schedIdx] = replaceScheduledTS(lines[schedIdx], newTS)
		lines = updateLastRepeat(lines, idx, now)
	} else {
		lines[idx] = setHeadlineState(lines[idx], doneState, allStates)
		closedStr := "CLOSED: " + formatInactiveTS(now)
		if schedIdx >= 0 {
			lines[schedIdx] = closedStr + " " + strings.TrimSpace(lines[schedIdx])
		} else {
			lines = sliceInsert(lines, idx+1, closedStr)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func fileTodoDef(lines []string) (active, done []string) {
	active = []string{"TODO", "WAIT"}
	done = []string{"DONE", "CANCELED"}
	for _, l := range lines {
		if m := todoDefRe.FindStringSubmatch(l); m != nil {
			return parseTodoDef(m[1])
		}
		if headlineRe.MatchString(l) {
			break
		}
	}
	return
}

func findScheduled(lines []string, headIdx int) (int, *Timestamp) {
	for i := headIdx + 1; i < len(lines) && i < headIdx+10; i++ {
		if headlineRe.MatchString(lines[i]) {
			break
		}
		if strings.Contains(lines[i], "SCHEDULED:") {
			ts := parseTimestamp(lines[i][strings.Index(lines[i], "SCHEDULED:"):])
			return i, ts
		}
	}
	return -1, nil
}

func setHeadlineState(line, newState string, allStates map[string]bool) string {
	m := headlineRe.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	stars := m[1]
	rest := m[2]
	if idx := strings.IndexByte(rest, ' '); idx > 0 {
		if allStates[rest[:idx]] {
			rest = rest[idx+1:]
		}
	} else if allStates[rest] {
		rest = ""
	}
	if rest == "" {
		return stars + " " + newState
	}
	return stars + " " + newState + " " + rest
}

func replaceScheduledTS(line, newTS string) string {
	si := strings.Index(line, "SCHEDULED:")
	if si < 0 {
		return line
	}
	after := line[si:]
	loc := timestampRe.FindStringIndex(after)
	if loc == nil {
		return line
	}
	return line[:si+loc[0]] + newTS + line[si+loc[1]:]
}

func updateLastRepeat(lines []string, headIdx int, now time.Time) []string {
	lr := formatInactiveTS(now)
	propIdx := -1
	for i := headIdx + 1; i < len(lines) && i < headIdx+15; i++ {
		if headlineRe.MatchString(lines[i]) {
			break
		}
		if strings.Contains(lines[i], ":LAST_REPEAT:") {
			lines[i] = ":LAST_REPEAT: " + lr
			return lines
		}
		if strings.TrimSpace(lines[i]) == ":PROPERTIES:" {
			propIdx = i
		}
	}
	if propIdx >= 0 {
		return sliceInsert(lines, propIdx+1, ":LAST_REPEAT: "+lr)
	}
	return lines
}

func advanceDate(ts *Timestamp, now time.Time) time.Time {
	prefix, num, unit := parseRepeater(ts.Repeater)

	add := func(t time.Time) time.Time {
		switch unit {
		case 'h':
			return t.Add(time.Duration(num) * time.Hour)
		case 'd':
			return t.AddDate(0, 0, num)
		case 'w':
			return t.AddDate(0, 0, num*7)
		case 'm':
			return t.AddDate(0, num, 0)
		case 'y':
			return t.AddDate(num, 0, 0)
		}
		return t
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if prefix == ".+" {
		base := time.Date(now.Year(), now.Month(), now.Day(),
			ts.Time.Hour(), ts.Time.Minute(), 0, 0, now.Location())
		return add(base)
	}

	next := add(ts.Time)
	for {
		nd := time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, now.Location())
		if !nd.Before(today) {
			break
		}
		next = add(next)
	}
	return next
}

func parseRepeater(rep string) (prefix string, num int, unit byte) {
	i := 0
	for i < len(rep) && (rep[i] == '+' || rep[i] == '.') {
		i++
	}
	prefix = rep[:i]
	rest := rep[i:]
	for j := 0; j < len(rest)-1; j++ {
		num = num*10 + int(rest[j]-'0')
	}
	unit = rest[len(rest)-1]
	return
}

func formatActiveTS(t time.Time, hasTime bool, repeater string) string {
	s := "<" + t.Format("2006-01-02 Mon")
	if hasTime {
		s += " " + t.Format("15:04")
	}
	if repeater != "" {
		s += " " + repeater
	}
	return s + ">"
}

func formatInactiveTS(t time.Time) string {
	return "[" + t.Format("2006-01-02 Mon 15:04") + "]"
}

func sliceInsert(s []string, idx int, val string) []string {
	s = append(s, "")
	copy(s[idx+1:], s[idx:])
	s[idx] = val
	return s
}

type ScheduleRef struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// RescheduleAll sets SCHEDULED date for each ref to newDate, preserving
// time-of-day and repeater. Entries without SCHEDULED get one inserted
// (inbox-style undated tasks). Lines are processed in descending order
// per file so insertions don't invalidate the remaining line numbers.
func RescheduleAll(dir string, refs []ScheduleRef, newDate time.Time) error {
	byFile := map[string][]int{}
	for _, r := range refs {
		if strings.Contains(r.File, "/") || strings.Contains(r.File, "..") {
			return fmt.Errorf("invalid file: %s", r.File)
		}
		byFile[r.File] = append(byFile[r.File], r.Line)
	}
	for file, lineNums := range byFile {
		path := filepath.Join(dir, file)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		lines := strings.Split(string(data), "\n")
		sort.Sort(sort.Reverse(sort.IntSlice(lineNums)))
		for _, ln := range lineNums {
			idx := ln - 1
			if idx < 0 || idx >= len(lines) {
				continue
			}
			if !headlineRe.MatchString(lines[idx]) {
				continue
			}
			lines = setScheduled(lines, idx, newDate)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", file, err)
		}
	}
	return nil
}

var scheduledPartRe = regexp.MustCompile(`\s*SCHEDULED:\s*<[^>]*>`)

// MoveToInbox clears the entry's SCHEDULED date and moves its subtree
// to inbox.org (entries already there just lose the date). Other
// planning info (CLOSED/DEADLINE) is kept. inbox.org is appended
// before the source is rewritten, so a mid-operation failure can only
// duplicate the entry, never lose it.
func MoveToInbox(dir, file string, line int) error {
	if strings.Contains(file, "/") || strings.Contains(file, "..") {
		return fmt.Errorf("invalid file: %s", file)
	}
	path := filepath.Join(dir, file)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	idx := line - 1
	if idx < 0 || idx >= len(lines) {
		return fmt.Errorf("line %d out of range", line)
	}
	m := headlineRe.FindStringSubmatch(lines[idx])
	if m == nil {
		return fmt.Errorf("line %d is not a headline", line)
	}
	level := len(m[1])
	endIdx := len(lines)
	for j := idx + 1; j < len(lines); j++ {
		if m2 := headlineRe.FindStringSubmatch(lines[j]); m2 != nil && len(m2[1]) <= level {
			endIdx = j
			break
		}
	}
	for endIdx > idx+1 && strings.TrimSpace(lines[endIdx-1]) == "" {
		endIdx--
	}

	block := make([]string, endIdx-idx)
	copy(block, lines[idx:endIdx])
	if schedIdx, _ := findScheduled(block, 0); schedIdx > 0 {
		nl := strings.TrimSpace(scheduledPartRe.ReplaceAllString(block[schedIdx], ""))
		if nl == "" {
			block = append(block[:schedIdx], block[schedIdx+1:]...)
		} else {
			block[schedIdx] = nl
		}
	}

	if file == "inbox.org" {
		rest := make([]string, len(lines[endIdx:]))
		copy(rest, lines[endIdx:])
		lines = append(append(lines[:idx], block...), rest...)
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
	}

	f, err := os.OpenFile(filepath.Join(dir, "inbox.org"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString("\n" + strings.Join(block, "\n") + "\n")
	f.Close()
	if err != nil {
		return err
	}
	lines = append(lines[:idx], lines[endIdx:]...)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func isPlanningLine(line string) bool {
	return strings.Contains(line, "SCHEDULED:") || strings.Contains(line, "DEADLINE:") || strings.Contains(line, "CLOSED:")
}

// setScheduled updates the SCHEDULED date of the headline at idx,
// preserving time-of-day and repeater, or inserts a SCHEDULED line
// when the entry has none.
func setScheduled(lines []string, idx int, newDate time.Time) []string {
	schedIdx, schedTS := findScheduled(lines, idx)
	if schedIdx >= 0 && schedTS != nil {
		newT := time.Date(newDate.Year(), newDate.Month(), newDate.Day(),
			schedTS.Time.Hour(), schedTS.Time.Minute(), 0, 0, newDate.Location())
		newTS := formatActiveTS(newT, schedTS.HasTime, schedTS.Repeater)
		lines[schedIdx] = replaceScheduledTS(lines[schedIdx], newTS)
		return lines
	}
	schedStr := "SCHEDULED: " + formatActiveTS(newDate, false, "")
	if idx+1 < len(lines) && isPlanningLine(lines[idx+1]) {
		lines[idx+1] = schedStr + " " + strings.TrimSpace(lines[idx+1])
	} else {
		lines = sliceInsert(lines, idx+1, schedStr)
	}
	return lines
}

type MoveScheduleRef struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Target string `json:"target"`
}

// ScheduleMove sets SCHEDULED to newDate for each ref and moves the
// entry's subtree to its Target file. An empty Target (or the source
// file itself) schedules in place. Source lines are processed in
// descending order so removals/insertions don't shift pending refs;
// targets are appended before the source is rewritten, so a failure
// in between can only duplicate an entry, never lose it.
func ScheduleMove(dir string, refs []MoveScheduleRef, newDate time.Time) error {
	byFile := map[string][]MoveScheduleRef{}
	for _, r := range refs {
		for _, n := range []string{r.File, r.Target} {
			if strings.Contains(n, "/") || strings.Contains(n, "..") {
				return fmt.Errorf("invalid file: %s", n)
			}
		}
		if r.Target != "" && !strings.HasSuffix(r.Target, ".org") {
			return fmt.Errorf("invalid target: %s", r.Target)
		}
		byFile[r.File] = append(byFile[r.File], r)
	}

	for file, frefs := range byFile {
		sort.Slice(frefs, func(i, j int) bool { return frefs[i].Line > frefs[j].Line })
		path := filepath.Join(dir, file)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		lines := strings.Split(string(data), "\n")
		var appends []struct {
			target string
			block  []string
		}

		for _, r := range frefs {
			idx := r.Line - 1
			if idx < 0 || idx >= len(lines) {
				continue
			}
			m := headlineRe.FindStringSubmatch(lines[idx])
			if m == nil {
				continue
			}
			if r.Target == "" || r.Target == file {
				lines = setScheduled(lines, idx, newDate)
				continue
			}

			level := len(m[1])
			endIdx := len(lines)
			for j := idx + 1; j < len(lines); j++ {
				if m2 := headlineRe.FindStringSubmatch(lines[j]); m2 != nil && len(m2[1]) <= level {
					endIdx = j
					break
				}
			}
			for endIdx > idx+1 && strings.TrimSpace(lines[endIdx-1]) == "" {
				endIdx--
			}

			block := make([]string, endIdx-idx)
			copy(block, lines[idx:endIdx])
			block = setScheduled(block, 0, newDate)
			appends = append(appends, struct {
				target string
				block  []string
			}{r.Target, block})

			lines = append(lines[:idx], lines[endIdx:]...)
		}

		for _, a := range appends {
			f, err := os.OpenFile(filepath.Join(dir, a.target), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				return fmt.Errorf("%s: %w", a.target, err)
			}
			_, err = f.WriteString("\n" + strings.Join(a.block, "\n") + "\n")
			f.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", a.target, err)
			}
		}

		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", file, err)
		}
	}
	return nil
}
